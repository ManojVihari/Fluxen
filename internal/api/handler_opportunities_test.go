package api

import (
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"fluxen/internal/store"
	"fluxen/pkg/types"
)

// seedOpenOpportunity inserts one real 'open' opportunity for app,
// bypassing the detector entirely — these tests exercise the review/
// dismiss transitions, not detection itself.
func seedOpenOpportunity(t *testing.T, pool *pgxpool.Pool, orgID types.OrgID, appID types.AppID) store.Opportunity {
	t.Helper()
	opp, err := store.NewOpportunities(pool).UpsertOpen(t.Context(), store.Opportunity{
		OrgID: orgID, AppID: appID, Kind: "model_cost", Fingerprint: "test-fingerprint",
		Title: "Test opportunity", Summary: "Seeded directly for a handler test.",
		Confidence: "high", ConfidenceScore: 0.9,
		Evidence: []byte(`{}`), Recommendation: []byte(`{}`),
		DetectorVersion: "test",
	})
	if err != nil {
		t.Fatalf("failed to seed opportunity: %v", err)
	}
	return opp
}

func TestOpportunity_DismissTransitionsStatusAndSetsReason(t *testing.T) {
	_, client, baseURL, pool := newTestEnv(t)
	app := setUpAndCreateApp(t, client, baseURL)

	sessResp := doJSON(t, client, http.MethodGet, baseURL+"/api/v1/auth/session", nil)
	var sess sessionResponse
	decodeJSON(t, sessResp, &sess)

	opp := seedOpenOpportunity(t, pool, types.OrgID(sess.OrgID), types.AppID(app.ID))

	resp := doJSON(t, client, http.MethodPost, baseURL+"/api/v1/opportunities/"+opp.ID+"/dismiss", dismissOpportunityRequest{
		Reason: "not a priority right now",
	})
	var dismissed opportunityResponse
	decodeJSON(t, resp, &dismissed)

	if dismissed.Status != "dismissed" {
		t.Fatalf("expected status=dismissed, got %q", dismissed.Status)
	}
	if dismissed.DismissedAt == nil {
		t.Error("expected dismissed_at to be set")
	}
	if dismissed.DismissReason == nil || *dismissed.DismissReason != "not a priority right now" {
		t.Errorf("expected dismiss_reason to round-trip, got %v", dismissed.DismissReason)
	}
}

func TestOpportunity_DismissWithoutReasonIsValid(t *testing.T) {
	_, client, baseURL, pool := newTestEnv(t)
	app := setUpAndCreateApp(t, client, baseURL)

	sessResp := doJSON(t, client, http.MethodGet, baseURL+"/api/v1/auth/session", nil)
	var sess sessionResponse
	decodeJSON(t, sessResp, &sess)

	opp := seedOpenOpportunity(t, pool, types.OrgID(sess.OrgID), types.AppID(app.ID))

	req, err := http.NewRequest(http.MethodPost, baseURL+"/api/v1/opportunities/"+opp.ID+"/dismiss", nil)
	if err != nil {
		t.Fatalf("failed to build request: %v", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for a body-less dismiss, got %d", resp.StatusCode)
	}
}

func TestOpportunity_DismissAlreadyAppliedReturns409(t *testing.T) {
	_, client, baseURL, pool := newTestEnv(t)
	app := setUpAndCreateApp(t, client, baseURL)

	sessResp := doJSON(t, client, http.MethodGet, baseURL+"/api/v1/auth/session", nil)
	var sess sessionResponse
	decodeJSON(t, sessResp, &sess)

	opportunities := store.NewOpportunities(pool)
	opp := seedOpenOpportunity(t, pool, types.OrgID(sess.OrgID), types.AppID(app.ID))
	if _, err := opportunities.MarkApplied(t.Context(), types.OrgID(sess.OrgID), opp.ID); err != nil {
		t.Fatalf("failed to mark applied: %v", err)
	}

	resp := doJSON(t, client, http.MethodPost, baseURL+"/api/v1/opportunities/"+opp.ID+"/dismiss", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409 dismissing an already-applied opportunity, got %d", resp.StatusCode)
	}
}
