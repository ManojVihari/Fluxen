package store

import (
	"context"
	"testing"
	"time"

	"fluxen/pkg/types"
)

// These tests seed the rollup tables directly (bypassing internal/rollup's
// compute step, which is tested on its own) so they can exercise the
// read-side queries in isolation with known, hand-checkable numbers.

func TestRollups_SummaryAndTimeseries(t *testing.T) {
	pool := newTestPool(t)
	rollups := NewRollups(pool)
	appID := types.AppID("55555555-5555-5555-5555-555555555555")

	day1 := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	day2 := time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC)

	_, err := pool.Exec(context.Background(), `
		INSERT INTO application_daily (app_id, day, requests, errors, input_tokens, output_tokens, total_tokens, cost_micro, duration_ms_sum)
		VALUES ($1, $2, 10, 1, 1000, 500, 1500, 200, 2000), ($1, $3, 20, 0, 2000, 1000, 3000, 400, 6000)
	`, appID, day1, day2)
	if err != nil {
		t.Fatalf("unexpected error seeding application_daily: %v", err)
	}

	since := day1
	until := day2.AddDate(0, 0, 1)

	summary, err := rollups.Summary(context.Background(), appID, since, until)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if summary.Requests != 30 {
		t.Errorf("expected 30 total requests, got %d", summary.Requests)
	}
	if summary.Errors != 1 {
		t.Errorf("expected 1 total error, got %d", summary.Errors)
	}
	if summary.CostMicro != 600 {
		t.Errorf("expected cost_micro=600, got %d", summary.CostMicro)
	}
	// avg duration = (2000+6000) / (10+20) = 266.67
	if summary.AvgDurationMS < 266 || summary.AvgDurationMS > 267 {
		t.Errorf("expected avg duration ~266.67ms, got %v", summary.AvgDurationMS)
	}

	points, err := rollups.Timeseries(context.Background(), appID, since, until)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(points) != 2 {
		t.Fatalf("expected 2 daily points, got %d", len(points))
	}
	if points[0].Requests != 10 || points[1].Requests != 20 {
		t.Errorf("expected points in chronological order [10,20], got [%d,%d]", points[0].Requests, points[1].Requests)
	}
}

func TestRollups_SummaryWithNoDataIsZeroNotError(t *testing.T) {
	pool := newTestPool(t)
	rollups := NewRollups(pool)

	summary, err := rollups.Summary(context.Background(), types.AppID("00000000-0000-0000-0000-000000000099"),
		time.Now().AddDate(0, 0, -30), time.Now())
	if err != nil {
		t.Fatalf("unexpected error for an application with no rollup data: %v", err)
	}
	if summary.Requests != 0 || summary.CostMicro != 0 {
		t.Errorf("expected a zero-valued summary, got %+v", summary)
	}
}

func TestRollups_ModelBreakdown_SortedByCostDescending(t *testing.T) {
	pool := newTestPool(t)
	rollups := NewRollups(pool)
	appID := types.AppID("66666666-6666-6666-6666-666666666666")
	day := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	orgID := "77777777-7777-7777-7777-777777777777"

	_, err := pool.Exec(context.Background(), `
		INSERT INTO request_rollup_daily (org_id, app_id, day, provider, model, status, requests, input_tokens, output_tokens, total_tokens, cost_micro, duration_ms_sum)
		VALUES
			($1, $2, $3, 'openai', 'gpt-4o-mini', 'ok', 100, 10000, 5000, 15000, 50, 20000),
			($1, $2, $3, 'openai', 'gpt-4o', 'ok', 20, 8000, 4000, 12000, 500, 8000)
	`, orgID, appID, day)
	if err != nil {
		t.Fatalf("unexpected error seeding request_rollup_daily: %v", err)
	}

	breakdown, err := rollups.ModelBreakdown(context.Background(), appID, day, day.AddDate(0, 0, 1))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(breakdown) != 2 {
		t.Fatalf("expected 2 models, got %d", len(breakdown))
	}
	if breakdown[0].Model != "gpt-4o" || breakdown[0].CostMicro != 500 {
		t.Errorf("expected gpt-4o (higher cost) first, got %+v", breakdown[0])
	}
	if breakdown[1].Model != "gpt-4o-mini" || breakdown[1].Requests != 100 {
		t.Errorf("expected gpt-4o-mini second with 100 requests, got %+v", breakdown[1])
	}
}

func TestOrganizations_First(t *testing.T) {
	pool := newTestPool(t)
	orgs := NewOrganizations(pool)

	_, found, err := orgs.First(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found {
		t.Fatal("expected no organization to be found before any is created")
	}

	created, err := orgs.Create(context.Background(), "Acme")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	first, found, err := orgs.First(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !found || first.ID != created.ID {
		t.Fatalf("expected First to return the created org, got found=%v %+v", found, first)
	}
}

func TestApplications_GetBySlug(t *testing.T) {
	pool := newTestPool(t)
	orgID := seedOrg(t, pool)
	apps := NewApplications(pool)

	created, err := apps.Create(context.Background(), orgID, "document-ai", "Document AI")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, err := apps.GetBySlug(context.Background(), orgID, "document-ai")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ID != created.ID {
		t.Errorf("expected GetBySlug to return the created application")
	}

	_, err = apps.GetBySlug(context.Background(), orgID, "does-not-exist")
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound for an unknown slug, got %v", err)
	}
}
