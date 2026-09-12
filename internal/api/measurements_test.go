package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"fluxen/internal/store"
	"fluxen/pkg/types"
)

func TestListMeasurements_EmptyBeforeAnyApply(t *testing.T) {
	_, client, baseURL, _ := newTestEnv(t)
	app := setUpAndCreateApp(t, client, baseURL)

	resp := doJSON(t, client, http.MethodGet, baseURL+"/api/v1/measurements?app_id="+app.ID, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var list []measurementResponse
	decodeJSON(t, resp, &list)
	if len(list) != 0 {
		t.Errorf("expected no measurements before any apply, got %+v", list)
	}
}

func TestApplyOpportunity_CreatesAMeasurementWithFrozenBaseline(t *testing.T) {
	_, client, baseURL, pool := newTestEnv(t)
	app := setUpAndCreateApp(t, client, baseURL)

	org, found, err := store.NewOrganizations(pool).First(context.Background())
	if err != nil || !found {
		t.Fatalf("unexpected error looking up seeded organization: found=%v err=%v", found, err)
	}
	orgID := org.ID
	appID := types.AppID(app.ID)

	// Baseline traffic: 100 requests at 1,000,000 micro each, well within
	// the 14 days before "now".
	reqs := store.NewRequests(pool)
	var records []types.UsageRecord
	now := time.Now().UTC()
	for i := 0; i < 100; i++ {
		records = append(records, types.UsageRecord{
			ID: fmt.Sprintf("00000000-0000-0000-0000-%012d", i+1), OrgID: orgID, AppID: appID,
			StartedAt: now.Add(-time.Duration(i+1) * time.Minute), DurationMS: 100,
			Endpoint: "chat.completions", Protocol: "openai", Streamed: false,
			RequestedModel: "gpt-4o", Provider: "openai", Model: "gpt-4o", RouteReason: "direct",
			InputTokens: 100, OutputTokens: 50, TotalTokens: 150, UsageSource: "provider",
			Cost: 1_000_000, CostStatus: types.CostKnown, PricingVersion: "test",
			CacheStatus: "disabled", Status: "ok", HTTPStatus: 200,
		})
	}
	if err := reqs.InsertBatch(context.Background(), records); err != nil {
		t.Fatalf("failed to seed baseline traffic: %v", err)
	}

	opps := store.NewOpportunities(pool)
	sims := store.NewSimulations(pool)
	opp, err := opps.UpsertOpen(context.Background(), store.Opportunity{
		OrgID: orgID, AppID: appID, Kind: "model_cost", Fingerprint: "fp-1",
		Title: "t", Summary: "s", WindowStart: now.Add(-14 * 24 * time.Hour), WindowEnd: now,
		SampleRequests: 100, CurrentCostMicro: 100, ProjectedCostMicro: 50, SavingsMicro: 50, SavingsPct: 0.3,
		Confidence: "medium", ConfidenceScore: 0.6,
		Evidence: json.RawMessage(`{}`), Recommendation: json.RawMessage(`{}`), DetectorVersion: "v1",
	})
	if err != nil {
		t.Fatalf("failed to seed opportunity: %v", err)
	}
	oppID := opp.ID
	sim, err := sims.Create(context.Background(), store.Simulation{
		OrgID: orgID, AppID: appID, OpportunityID: &oppID,
		Scenario: json.RawMessage(`{"type":"model_mix"}`), WindowStart: now.Add(-14 * 24 * time.Hour), WindowEnd: now,
		Breakdown: json.RawMessage(`[]`), Assumptions: json.RawMessage(`[]`), EngineVersion: "v1",
	})
	if err != nil {
		t.Fatalf("failed to seed simulation: %v", err)
	}

	applyResp := doJSON(t, client, http.MethodPost, baseURL+"/api/v1/opportunities/"+opp.ID+"/apply", applyOpportunityRequest{
		Confirm: true, SimulationID: sim.ID,
		Routing: applyRoutingRequest{FromModel: "gpt-4o", ToModel: "gpt-4o-mini", Weight: 0.5, Sticky: true},
	})
	if applyResp.StatusCode != http.StatusOK {
		t.Fatalf("expected apply to succeed with 200, got %d", applyResp.StatusCode)
	}

	listResp := doJSON(t, client, http.MethodGet, baseURL+"/api/v1/measurements?app_id="+app.ID, nil)
	var list []measurementResponse
	decodeJSON(t, listResp, &list)
	if len(list) != 1 {
		t.Fatalf("expected exactly one measurement after apply, got %+v", list)
	}
	m := list[0]
	if m.Status != "collecting" {
		t.Errorf("expected status=collecting immediately after apply, got %q", m.Status)
	}
	if m.BaselineRequests != 100 {
		t.Errorf("expected baseline_requests=100, got %d", m.BaselineRequests)
	}
	if m.BaselineCostPer1kMicro != 1_000_000_000 {
		t.Errorf("expected baseline_cost_per_1k_micro=1,000,000,000 (total cost 100,000,000 micro / 100 requests * 1000), got %d", m.BaselineCostPer1kMicro)
	}
	if m.OpportunityID != opp.ID {
		t.Errorf("expected the measurement to link back to the opportunity, got %q", m.OpportunityID)
	}

	getResp := doJSON(t, client, http.MethodGet, baseURL+"/api/v1/measurements/"+m.ID, nil)
	var got measurementResponse
	decodeJSON(t, getResp, &got)
	if got.ID != m.ID {
		t.Errorf("expected GET to return the same measurement, got %+v", got)
	}

	byOppResp := doJSON(t, client, http.MethodGet, baseURL+"/api/v1/opportunities/"+opp.ID+"/measurement", nil)
	var byOpp measurementResponse
	decodeJSON(t, byOppResp, &byOpp)
	if byOpp.ID != m.ID {
		t.Errorf("expected the opportunity-scoped lookup to return the same measurement, got %+v", byOpp)
	}
}

func TestRevertMeasurement_RequiresConfirm(t *testing.T) {
	_, client, baseURL, _ := newTestEnv(t)
	setUpAndCreateApp(t, client, baseURL)

	resp := doJSON(t, client, http.MethodPost, baseURL+"/api/v1/measurements/does-not-matter/revert", revertMeasurementRequest{Confirm: false})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 when confirm is false, got %d", resp.StatusCode)
	}
}
