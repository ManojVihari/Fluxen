package api

import (
	"context"
	"net/http"
	"testing"
	"time"
)

// signUpAndCreateApp is the shared setup path for tests that need one
// logged-in owner plus one application, without exercising the rest of
// TestFullJourney's steps.
func signUpAndCreateApp(t *testing.T, client *http.Client, baseURL, appName string) applicationResponse {
	t.Helper()
	resp := doJSON(t, client, http.MethodPost, baseURL+"/api/v1/setup", setupRequest{
		OrgName: "Acme", Email: "owner@example.com", Password: "supersecret123",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 from setup, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	appResp := doJSON(t, client, http.MethodPost, baseURL+"/api/v1/applications", createApplicationRequest{Name: appName})
	if appResp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 creating an application, got %d", appResp.StatusCode)
	}
	var app applicationResponse
	decodeJSON(t, appResp, &app)
	return app
}

func TestOverview_ReturnsOrgWideAggregates(t *testing.T) {
	_, client, baseURL, pool := newTestEnv(t)
	app := signUpAndCreateApp(t, client, baseURL, "Document AI")

	day := time.Now().UTC().AddDate(0, 0, -1)
	day = time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, time.UTC)
	_, err := pool.Exec(context.Background(), `
		INSERT INTO application_daily (app_id, day, requests, errors, input_tokens, output_tokens, total_tokens, cost_micro, duration_ms_sum)
		VALUES ($1, $2, 50, 2, 500, 250, 750, 5000, 2500)
	`, app.ID, day)
	if err != nil {
		t.Fatalf("unexpected error seeding application_daily: %v", err)
	}

	resp := doJSON(t, client, http.MethodGet, baseURL+"/api/v1/overview?range=30d", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var out map[string]any
	decodeJSON(t, resp, &out)

	if out["requests"].(float64) != 50 {
		t.Errorf("expected requests=50, got %v", out["requests"])
	}
	if out["cost_micro"].(float64) != 5000 {
		t.Errorf("expected cost_micro=5000, got %v", out["cost_micro"])
	}
	if _, ok := out["top_applications"]; !ok {
		t.Error("expected a top_applications field")
	}
	if _, ok := out["opportunity_feed"]; !ok {
		t.Error("expected an opportunity_feed field")
	}

	tsResp := doJSON(t, client, http.MethodGet, baseURL+"/api/v1/overview/timeseries?range=30d", nil)
	if tsResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", tsResp.StatusCode)
	}
	var points []overviewDailyPointResponse
	decodeJSON(t, tsResp, &points)
	if len(points) != 1 || points[0].CostMicro != 5000 {
		t.Errorf("expected one timeseries point with cost_micro=5000, got %+v", points)
	}
}

// TestOverview_CountsReviewedOpportunitiesAsLive is a regression test:
// potential_savings_micro and the opportunity_feed must include
// "reviewed" (and "simulated") opportunities, not just literal
// status="open" — Part G.1's own definition of "live, not yet resolved"
// — mirroring the same status set store.Opportunities.MarkApplied treats
// as applicable. An earlier version of handleOverview queried only
// status="open" and silently dropped every opportunity a user had
// already opened (auto-transitioned to "reviewed" on first view).
func TestOverview_CountsReviewedOpportunitiesAsLive(t *testing.T) {
	_, client, baseURL, pool := newTestEnv(t)
	app := signUpAndCreateApp(t, client, baseURL, "Document AI")

	orgResp := doJSON(t, client, http.MethodGet, baseURL+"/api/v1/auth/session", nil)
	var session sessionResponse
	decodeJSON(t, orgResp, &session)

	now := time.Now().UTC()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO opportunities (
			org_id, app_id, kind, fingerprint, status, title, summary,
			window_start, window_end, sample_requests,
			current_cost_micro, projected_cost_micro, savings_micro, savings_pct,
			confidence, confidence_score, evidence, recommendation, detector_version
		) VALUES (
			$1, $2, 'model_cost', 'fp-reviewed', 'reviewed', 'Switch to a cheaper model', 'summary',
			$3, $4, 1000,
			100000000, 70000000, 30000000, 0.3,
			'high', 1.0, '{}', '{}', 'v1'
		)
	`, session.OrgID, app.ID, now.AddDate(0, 0, -14), now)
	if err != nil {
		t.Fatalf("unexpected error seeding a reviewed opportunity: %v", err)
	}

	resp := doJSON(t, client, http.MethodGet, baseURL+"/api/v1/overview", nil)
	var out struct {
		PotentialSavingsMicro float64                  `json:"potential_savings_micro"`
		OpportunityFeed       []overviewOpportunityRow `json:"opportunity_feed"`
	}
	decodeJSON(t, resp, &out)

	if out.PotentialSavingsMicro != 30000000 {
		t.Errorf("expected potential_savings_micro to include the reviewed opportunity's 30000000, got %v", out.PotentialSavingsMicro)
	}
	if len(out.OpportunityFeed) != 1 || out.OpportunityFeed[0].SavingsMicro != 30000000 {
		t.Errorf("expected the reviewed opportunity in the feed, got %+v", out.OpportunityFeed)
	}
}

func TestOverview_RequiresAuth(t *testing.T) {
	_, client, baseURL, _ := newTestEnv(t)

	resp := doJSON(t, client, http.MethodGet, baseURL+"/api/v1/overview", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 without a session, got %d", resp.StatusCode)
	}
}
