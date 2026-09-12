package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"fluxen/internal/store"
	corepolicy "fluxen/pkg/policy"
	"fluxen/pkg/types"
)

func TestGetPolicy_DefaultsToEmptyDocumentAtVersionZero(t *testing.T) {
	_, client, baseURL, _ := newTestEnv(t)
	app := setUpAndCreateApp(t, client, baseURL)

	resp := doJSON(t, client, http.MethodGet, baseURL+"/api/v1/applications/"+app.ID+"/policy", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var got policyResponse
	decodeJSON(t, resp, &got)
	if got.Version != 0 {
		t.Errorf("expected version 0 for an app with no policy, got %d", got.Version)
	}
}

func TestPutPolicy_SavesAndAppearsInHistory(t *testing.T) {
	_, client, baseURL, _ := newTestEnv(t)
	app := setUpAndCreateApp(t, client, baseURL)

	body := putPolicyRequest{
		Document:        corepolicy.PolicyDocument{RateLimit: &corepolicy.RateLimitPolicy{Enabled: true, RequestsPerMinute: 60}},
		ExpectedVersion: 0,
		Note:            "turn on rate limiting",
	}
	resp := doJSON(t, client, http.MethodPut, baseURL+"/api/v1/applications/"+app.ID+"/policy", body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var saved policyResponse
	decodeJSON(t, resp, &saved)
	if saved.Version != 1 || saved.Document.RateLimit == nil || saved.Document.RateLimit.RequestsPerMinute != 60 {
		t.Fatalf("expected the saved policy to reflect the request, got %+v", saved)
	}

	historyResp := doJSON(t, client, http.MethodGet, baseURL+"/api/v1/applications/"+app.ID+"/policy/history", nil)
	var history []policyHistoryResponse
	decodeJSON(t, historyResp, &history)
	if len(history) != 1 || history[0].ChangeSource != "user" {
		t.Fatalf("expected one user-sourced history entry, got %+v", history)
	}
}

func TestPutPolicy_RejectsStaleVersion(t *testing.T) {
	_, client, baseURL, _ := newTestEnv(t)
	app := setUpAndCreateApp(t, client, baseURL)

	first := putPolicyRequest{Document: corepolicy.PolicyDocument{RateLimit: &corepolicy.RateLimitPolicy{Enabled: true, RequestsPerMinute: 60}}, ExpectedVersion: 0}
	doJSON(t, client, http.MethodPut, baseURL+"/api/v1/applications/"+app.ID+"/policy", first).Body.Close()

	// Same expected_version=0 again — stale, since the save above already moved it to 1.
	second := putPolicyRequest{Document: corepolicy.PolicyDocument{RateLimit: &corepolicy.RateLimitPolicy{Enabled: true, RequestsPerMinute: 30}}, ExpectedVersion: 0}
	resp := doJSON(t, client, http.MethodPut, baseURL+"/api/v1/applications/"+app.ID+"/policy", second)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409 for a stale expected_version, got %d", resp.StatusCode)
	}
}

func TestPutPolicy_RejectsInvalidDocument(t *testing.T) {
	_, client, baseURL, _ := newTestEnv(t)
	app := setUpAndCreateApp(t, client, baseURL)

	body := putPolicyRequest{Document: corepolicy.PolicyDocument{RateLimit: &corepolicy.RateLimitPolicy{Enabled: true, RequestsPerMinute: 0}}}
	resp := doJSON(t, client, http.MethodPut, baseURL+"/api/v1/applications/"+app.ID+"/policy", body)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for an invalid document (zero requests_per_minute), got %d", resp.StatusCode)
	}
}

func TestRevertPolicy_RestoresAnEarlierVersionAsANewOne(t *testing.T) {
	_, client, baseURL, _ := newTestEnv(t)
	app := setUpAndCreateApp(t, client, baseURL)

	v1 := putPolicyRequest{Document: corepolicy.PolicyDocument{RateLimit: &corepolicy.RateLimitPolicy{Enabled: true, RequestsPerMinute: 60}}, ExpectedVersion: 0}
	resp1 := doJSON(t, client, http.MethodPut, baseURL+"/api/v1/applications/"+app.ID+"/policy", v1)
	var saved1 policyResponse
	decodeJSON(t, resp1, &saved1)

	v2 := putPolicyRequest{Document: corepolicy.PolicyDocument{RateLimit: &corepolicy.RateLimitPolicy{Enabled: true, RequestsPerMinute: 10}}, ExpectedVersion: saved1.Version}
	doJSON(t, client, http.MethodPut, baseURL+"/api/v1/applications/"+app.ID+"/policy", v2).Body.Close()

	revertResp := doJSON(t, client, http.MethodPost, baseURL+"/api/v1/applications/"+app.ID+"/policy/revert", revertPolicyRequest{Version: saved1.Version})
	if revertResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", revertResp.StatusCode)
	}
	var reverted policyResponse
	decodeJSON(t, revertResp, &reverted)
	if reverted.Version != 3 {
		t.Errorf("expected the revert to land as version 3 (a new version, never rewriting history), got %d", reverted.Version)
	}
	if reverted.Document.RateLimit.RequestsPerMinute != 60 {
		t.Errorf("expected the reverted document to match version 1's content (60 rpm), got %+v", reverted.Document)
	}

	historyResp := doJSON(t, client, http.MethodGet, baseURL+"/api/v1/applications/"+app.ID+"/policy/history", nil)
	var history []policyHistoryResponse
	decodeJSON(t, historyResp, &history)
	if len(history) != 3 || history[0].ChangeSource != "revert" {
		t.Fatalf("expected 3 history entries with the newest sourced from revert, got %+v", history)
	}
}

func TestApplyOpportunity_EndToEnd(t *testing.T) {
	_, client, baseURL, pool := newTestEnv(t)
	app := setUpAndCreateApp(t, client, baseURL)

	orgID, found, err := store.NewOrganizations(pool).First(context.Background())
	if err != nil || !found {
		t.Fatalf("unexpected error looking up seeded organization: found=%v err=%v", found, err)
	}
	opps := store.NewOpportunities(pool)
	sims := store.NewSimulations(pool)

	opp, err := opps.UpsertOpen(context.Background(), store.Opportunity{
		OrgID: orgID.ID, AppID: types.AppID(app.ID), Kind: "model_cost", Fingerprint: "fp-1",
		Title: "t", Summary: "s", WindowStart: time.Now().Add(-14 * 24 * time.Hour), WindowEnd: time.Now(),
		SampleRequests: 1000, CurrentCostMicro: 100, ProjectedCostMicro: 50, SavingsMicro: 50, SavingsPct: 0.5,
		Confidence: "medium", ConfidenceScore: 0.6,
		Evidence: json.RawMessage(`{}`), Recommendation: json.RawMessage(`{}`), DetectorVersion: "v1",
	})
	if err != nil {
		t.Fatalf("unexpected error seeding opportunity: %v", err)
	}
	oppID := opp.ID
	sim, err := sims.Create(context.Background(), store.Simulation{
		OrgID: orgID.ID, AppID: types.AppID(app.ID), OpportunityID: &oppID,
		Scenario: json.RawMessage(`{"type":"model_mix"}`), WindowStart: opp.WindowStart, WindowEnd: opp.WindowEnd,
		Breakdown: json.RawMessage(`[]`), Assumptions: json.RawMessage(`[]`), EngineVersion: "v1",
	})
	if err != nil {
		t.Fatalf("unexpected error seeding simulation: %v", err)
	}

	applyBody := applyOpportunityRequest{
		Confirm: true, SimulationID: sim.ID,
		Routing: applyRoutingRequest{FromModel: "gpt-4o", ToModel: "gpt-4o-mini", Weight: 0.5, Sticky: true},
		Note:    "applying the aha moment",
	}
	resp := doJSON(t, client, http.MethodPost, baseURL+"/api/v1/opportunities/"+opp.ID+"/apply", applyBody)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var applied policyResponse
	decodeJSON(t, resp, &applied)
	if applied.Document.Routing == nil || applied.Document.Routing.ToModel != "gpt-4o-mini" {
		t.Fatalf("expected the applied policy to carry the routing patch, got %+v", applied.Document)
	}

	oppAfter, err := opps.Get(context.Background(), orgID.ID, opp.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if oppAfter.Status != "applied" {
		t.Errorf("expected the opportunity to transition to applied, got %q", oppAfter.Status)
	}
}

func TestApplyOpportunity_RequiresConfirm(t *testing.T) {
	_, client, baseURL, _ := newTestEnv(t)
	setUpAndCreateApp(t, client, baseURL)

	resp := doJSON(t, client, http.MethodPost, baseURL+"/api/v1/opportunities/does-not-matter/apply", applyOpportunityRequest{
		Confirm: false, SimulationID: "sim-1",
		Routing: applyRoutingRequest{FromModel: "gpt-4o", ToModel: "gpt-4o-mini", Weight: 0.5},
	})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 when confirm is false, got %d", resp.StatusCode)
	}
}
