package api

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"fluxen/internal/store"
	"fluxen/pkg/pricing"
	"fluxen/pkg/types"
)

// seedRequestsForSimulation inserts n real requests for an app, all
// served by model, with the given per-request token shape, ending "now"
// and spread one minute apart — enough for a simulation to replay
// chronologically without needing a full trafficgen-scale fixture.
func seedRequestsForSimulation(t *testing.T, reqs *store.Requests, orgID types.OrgID, appID types.AppID, model string, n, inputTokens, outputTokens int) {
	t.Helper()
	catalog, err := pricing.LoadEmbedded()
	if err != nil {
		t.Fatalf("failed to load catalog: %v", err)
	}
	usage := types.ResponseUsage{InputTokens: inputTokens, OutputTokens: outputTokens, TotalTokens: inputTokens + outputTokens}
	_, _, total, status := pricing.Calculate(catalog, model, usage)

	now := time.Now().UTC()
	var records []types.UsageRecord
	for i := 0; i < n; i++ {
		records = append(records, types.UsageRecord{
			ID: fmt.Sprintf("00000000-0000-0000-0000-%012d", i+1), OrgID: orgID, AppID: appID,
			StartedAt: now.Add(-time.Duration(n-i) * time.Minute), DurationMS: 500,
			Endpoint: "chat.completions", Protocol: "openai", Streamed: false,
			RequestedModel: model, Provider: "openai", Model: model, RouteReason: "direct",
			InputTokens: inputTokens, OutputTokens: outputTokens, TotalTokens: inputTokens + outputTokens,
			UsageSource: "provider",
			Cost:        total, CostStatus: status, PricingVersion: catalog.Version,
			CacheStatus: "disabled", Status: "ok", HTTPStatus: 200,
		})
	}
	if err := reqs.InsertBatch(context.Background(), records); err != nil {
		t.Fatalf("failed to seed requests: %v", err)
	}
}

func TestCreateSimulation_ModelMix_MatchesHandComputedNumbers(t *testing.T) {
	_, client, baseURL, pool := newTestEnv(t)
	app := setUpAndCreateApp(t, client, baseURL)

	reqs := store.NewRequests(pool)
	org, found, err := store.NewOrganizations(pool).First(context.Background())
	if err != nil || !found {
		t.Fatalf("failed to look up seeded organization: found=%v err=%v", found, err)
	}
	// 10 gpt-4o requests, 1000 in / 100 out each: 3500 micro each = 35000 total.
	seedRequestsForSimulation(t, reqs, org.ID, types.AppID(app.ID), "gpt-4o", 10, 1000, 100)

	body := map[string]any{
		"app_id":      app.ID,
		"window_days": 30,
		"scenario": map[string]any{
			"type": "model_mix", "current_model": "gpt-4o", "candidate_model": "gpt-4o-mini", "traffic_weight": 0.5,
		},
	}
	resp := doJSON(t, client, http.MethodPost, baseURL+"/api/v1/simulations", body)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}
	var sim simulationResponse
	decodeJSON(t, resp, &sim)

	if sim.ReplayedRequests != 10 {
		t.Errorf("expected 10 replayed requests, got %d", sim.ReplayedRequests)
	}
	if sim.AffectedRequests != 5 {
		t.Errorf("expected 5 affected requests at weight 0.5, got %d", sim.AffectedRequests)
	}
	if sim.ActualCostMicro != 35000 {
		t.Errorf("expected actual cost 35000, got %d", sim.ActualCostMicro)
	}
	wantSimulated := int64(5*3500 + 5*210)
	if sim.SimulatedCostMicro != wantSimulated {
		t.Errorf("expected simulated cost %d, got %d", wantSimulated, sim.SimulatedCostMicro)
	}
	if sim.DeltaMicro != sim.SimulatedCostMicro-sim.ActualCostMicro {
		t.Error("expected delta_micro = simulated - actual")
	}
	if sim.ValueType != "estimated" {
		t.Errorf("expected value_type=estimated, got %q", sim.ValueType)
	}

	// GET by id must return the same result.
	getResp := doJSON(t, client, http.MethodGet, baseURL+"/api/v1/simulations/"+sim.ID, nil)
	var fetched simulationResponse
	decodeJSON(t, getResp, &fetched)
	if fetched.ID != sim.ID || fetched.SimulatedCostMicro != sim.SimulatedCostMicro {
		t.Errorf("expected GET to return the same simulation, got %+v", fetched)
	}

	// It must also show up in the application's simulation list.
	listResp := doJSON(t, client, http.MethodGet, baseURL+"/api/v1/applications/"+app.ID+"/simulations", nil)
	var list []simulationResponse
	decodeJSON(t, listResp, &list)
	if len(list) != 1 || list[0].ID != sim.ID {
		t.Errorf("expected the simulation to appear in the application's list, got %+v", list)
	}
}

func TestCreateSimulation_RejectsUnsupportedScenarioType(t *testing.T) {
	_, client, baseURL, _ := newTestEnv(t)
	app := setUpAndCreateApp(t, client, baseURL)

	body := map[string]any{
		"app_id": app.ID, "window_days": 30,
		"scenario": map[string]any{"type": "budget"},
	}
	resp := doJSON(t, client, http.MethodPost, baseURL+"/api/v1/simulations", body)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for an unsupported (Phase 5) scenario type, got %d", resp.StatusCode)
	}
}

func TestCreateSimulation_UnknownApplicationIs404(t *testing.T) {
	_, client, baseURL, _ := newTestEnv(t)
	doJSON(t, client, http.MethodPost, baseURL+"/api/v1/setup", setupRequest{
		OrgName: "Acme", Email: "owner@example.com", Password: "supersecret123",
	}).Body.Close()

	body := map[string]any{
		"app_id": "00000000-0000-0000-0000-000000000000", "window_days": 30,
		"scenario": map[string]any{"type": "model_mix", "current_model": "gpt-4o", "candidate_model": "gpt-4o-mini", "traffic_weight": 0.5},
	}
	resp := doJSON(t, client, http.MethodPost, baseURL+"/api/v1/simulations", body)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 for an unknown application, got %d", resp.StatusCode)
	}
}
