package store

import (
	"context"
	"testing"
	"time"

	"fluxen/pkg/types"
)

func TestOverview_SummaryTimeseriesAndProviderMix(t *testing.T) {
	pool := newTestPool(t)
	orgID := seedOrg(t, pool)
	apps := NewApplications(pool)
	overview := NewOverview(pool)

	app1, err := apps.Create(context.Background(), orgID, "app-one", "App One")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	app2, err := apps.Create(context.Background(), orgID, "app-two", "App Two")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	day1 := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	day2 := time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC)

	_, err = pool.Exec(context.Background(), `
		INSERT INTO application_daily (app_id, day, requests, errors, input_tokens, output_tokens, total_tokens, cost_micro, duration_ms_sum)
		VALUES ($1, $3, 10, 0, 100, 50, 150, 1000, 500), ($2, $3, 20, 1, 200, 100, 300, 2000, 1000), ($1, $4, 5, 0, 50, 25, 75, 500, 250)
	`, app1.ID, app2.ID, day1, day2)
	if err != nil {
		t.Fatalf("unexpected error seeding application_daily: %v", err)
	}

	_, err = pool.Exec(context.Background(), `
		INSERT INTO request_rollup_daily (org_id, app_id, day, provider, model, status, requests, input_tokens, output_tokens, total_tokens, cost_micro, duration_ms_sum)
		VALUES ($1, $2, $4, 'openai', 'gpt-4o-mini', 'ok', 10, 100, 50, 150, 1000, 500), ($1, $3, $4, 'gemini', 'gemini-1.5-flash', 'ok', 20, 200, 100, 300, 2000, 1000)
	`, orgID, app1.ID, app2.ID, day1)
	if err != nil {
		t.Fatalf("unexpected error seeding request_rollup_daily: %v", err)
	}

	since, until := day1, day2.AddDate(0, 0, 1)
	summary, err := overview.Summary(context.Background(), orgID, since, until)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if summary.Requests != 35 || summary.CostMicro != 3500 {
		t.Errorf("expected requests=35 cost=3500, got %+v", summary)
	}

	points, err := overview.Timeseries(context.Background(), orgID, since, until)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(points) != 2 || points[0].CostMicro != 3000 || points[1].CostMicro != 500 {
		t.Errorf("expected [3000,500] chronological, got %+v", points)
	}

	mix, err := overview.ProviderMix(context.Background(), orgID, since, until)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mix) != 2 || mix[0].Provider != "gemini" || mix[0].CostMicro != 2000 {
		t.Errorf("expected gemini first with cost 2000 (highest), got %+v", mix)
	}
}

func TestOverview_TopApplications_RankedByCurrentSpendWithPriorAndOpportunityValue(t *testing.T) {
	pool := newTestPool(t)
	orgID := seedOrg(t, pool)
	apps := NewApplications(pool)
	opportunities := NewOpportunities(pool)
	overview := NewOverview(pool)

	big, err := apps.Create(context.Background(), orgID, "big-spender", "Big Spender")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	small, err := apps.Create(context.Background(), orgID, "small-spender", "Small Spender")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	currentStart := time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC)
	currentEnd := currentStart.AddDate(0, 0, 14)
	priorStart := currentStart.AddDate(0, 0, -14)

	_, err = pool.Exec(context.Background(), `
		INSERT INTO application_daily (app_id, day, requests, errors, input_tokens, output_tokens, total_tokens, cost_micro, duration_ms_sum)
		VALUES
			($1, $3, 100, 0, 1000, 500, 1500, 100000, 5000),
			($1, $4, 100, 0, 1000, 500, 1500, 80000, 5000),
			($2, $3, 10, 0, 100, 50, 150, 1000, 500)
	`, big.ID, small.ID, currentStart, priorStart)
	if err != nil {
		t.Fatalf("unexpected error seeding application_daily: %v", err)
	}

	sev := "medium"
	_, err = opportunities.UpsertOpen(context.Background(), Opportunity{
		OrgID: orgID, AppID: big.ID, Kind: "model_cost", Fingerprint: "fp-1", Severity: &sev,
		Title: "t", Summary: "s", WindowStart: currentStart, WindowEnd: currentEnd, SampleRequests: 100,
		CurrentCostMicro: 100000, ProjectedCostMicro: 70000, SavingsMicro: 30000, SavingsPct: 0.3,
		Confidence: "high", ConfidenceScore: 1.0, Evidence: []byte(`{}`), Recommendation: []byte(`{}`),
		DetectorVersion: "v1",
	})
	if err != nil {
		t.Fatalf("unexpected error creating opportunity: %v", err)
	}

	top, err := overview.TopApplications(context.Background(), orgID, currentStart, currentEnd, priorStart, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(top) != 2 {
		t.Fatalf("expected 2 applications, got %d", len(top))
	}
	if top[0].AppID != big.ID {
		t.Fatalf("expected the big spender ranked first, got %+v", top[0])
	}
	if top[0].CostMicro != 100000 || top[0].PriorCostMicro != 80000 {
		t.Errorf("expected current=100000 prior=80000, got current=%d prior=%d", top[0].CostMicro, top[0].PriorCostMicro)
	}
	if top[0].OpenOpportunityValue != 30000 {
		t.Errorf("expected open opportunity value 30000, got %d", top[0].OpenOpportunityValue)
	}
	if top[0].EfficiencyScore != nil {
		t.Errorf("expected a nil efficiency score (none computed yet), got %v", *top[0].EfficiencyScore)
	}
	if top[1].AppID != small.ID || top[1].OpenOpportunityValue != 0 {
		t.Errorf("expected the small spender second with zero opportunity value, got %+v", top[1])
	}
}

func TestOverview_SummaryWithNoAppsIsZeroNotError(t *testing.T) {
	pool := newTestPool(t)
	orgID := seedOrg(t, pool)
	overview := NewOverview(pool)

	summary, err := overview.Summary(context.Background(), orgID, time.Now().AddDate(0, 0, -30), time.Now())
	if err != nil {
		t.Fatalf("unexpected error for an org with no applications: %v", err)
	}
	if summary.Requests != 0 || summary.CostMicro != types.Money(0) {
		t.Errorf("expected a zero-valued summary, got %+v", summary)
	}
}
