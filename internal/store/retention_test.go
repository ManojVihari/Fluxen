package store

import (
	"context"
	"testing"
	"time"
)

func TestOrganizations_RetentionSettingsDefaultsAndUpdate(t *testing.T) {
	pool := newTestPool(t)
	orgID := seedOrg(t, pool)
	orgs := NewOrganizations(pool)

	s, err := orgs.GetRetentionSettings(context.Background(), orgID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.RequestsRetentionDays != 90 || s.BodyRetentionDays != 7 || s.BodyCaptureEnabled {
		t.Errorf("expected defaults 90/7/false, got %+v", s)
	}

	err = orgs.UpdateRetentionSettings(context.Background(), orgID, RetentionSettings{
		RequestsRetentionDays: 30, BodyRetentionDays: 3, BodyCaptureEnabled: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, err := orgs.GetRetentionSettings(context.Background(), orgID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.RequestsRetentionDays != 30 || got.BodyRetentionDays != 3 || !got.BodyCaptureEnabled {
		t.Errorf("expected updated 30/3/true, got %+v", got)
	}
}

func TestRetention_EnforceRequestsDropsOldRowsAndClearsBodies(t *testing.T) {
	pool := newTestPool(t)
	orgID := seedOrg(t, pool)
	apps := NewApplications(pool)
	app, err := apps.Create(context.Background(), orgID, "app-one", "App One")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	now := time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC)
	old := now.AddDate(0, 0, -100)   // outside both windows
	midAge := now.AddDate(0, 0, -30) // outside body window (7d default-ish), inside requests window
	recent := now.AddDate(0, 0, -1)  // inside both windows

	insert := func(id string, startedAt time.Time) {
		_, err := pool.Exec(context.Background(), `
			INSERT INTO requests (
				id, org_id, app_id, started_at, duration_ms, endpoint, protocol, streamed,
				requested_model, provider, model, status, cost_status, cache_status,
				request_body, response_body
			) VALUES ($1, $2, $3, $4, 100, 'chat.completions', 'openai', false, 'gpt-4o-mini', 'openai', 'gpt-4o-mini', 'ok', 'known', 'miss', '{}', '{}')
		`, id, orgID, app.ID, startedAt)
		if err != nil {
			t.Fatalf("unexpected error seeding request %s: %v", id, err)
		}
	}
	insert("11111111-1111-1111-1111-111111111111", old)
	insert("22222222-2222-2222-2222-222222222222", midAge)
	insert("33333333-3333-3333-3333-333333333333", recent)

	retention := NewRetention(pool)
	settings := RetentionSettings{RequestsRetentionDays: 90, BodyRetentionDays: 14, BodyCaptureEnabled: true}

	dropped, cleared, err := retention.EnforceRequests(context.Background(), orgID, settings, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dropped != 1 {
		t.Errorf("expected 1 dropped request (older than 90d), got %d", dropped)
	}
	if cleared != 1 {
		t.Errorf("expected 1 request with its body cleared (older than 14d but within 90d), got %d", cleared)
	}

	var remaining int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM requests WHERE org_id = $1`, orgID).Scan(&remaining); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if remaining != 2 {
		t.Errorf("expected 2 remaining requests, got %d", remaining)
	}

	var midBody, midBody2 *string
	if err := pool.QueryRow(context.Background(), `SELECT request_body::text, response_body::text FROM requests WHERE id = $1`, "22222222-2222-2222-2222-222222222222").Scan(&midBody, &midBody2); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if midBody != nil || midBody2 != nil {
		t.Error("expected the mid-age request's body to be cleared")
	}

	var recentBody *string
	if err := pool.QueryRow(context.Background(), `SELECT request_body::text FROM requests WHERE id = $1`, "33333333-3333-3333-3333-333333333333").Scan(&recentBody); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if recentBody == nil {
		t.Error("expected the recent request's body to still be present")
	}
}

func TestRetention_EnforceOpportunitiesDropsOldDismissedAndStale(t *testing.T) {
	pool := newTestPool(t)
	orgID := seedOrg(t, pool)
	apps := NewApplications(pool)
	app, err := apps.Create(context.Background(), orgID, "app-one", "App One")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	now := time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC)
	old := now.AddDate(0, 0, -200)
	recent := now.AddDate(0, 0, -10)

	insertOpp := func(id, status string, detectedAt time.Time) {
		_, err := pool.Exec(context.Background(), `
			INSERT INTO opportunities (
				id, org_id, app_id, kind, fingerprint, status, title, summary,
				window_start, window_end, sample_requests,
				current_cost_micro, projected_cost_micro, savings_micro, savings_pct,
				confidence, confidence_score, evidence, recommendation, detector_version, detected_at
			) VALUES ($1, $2, $3, 'model_cost', $6, $4, 't', 's', $5, $5, 1, 0, 0, 0, 0, 'low', 0.3, '{}', '{}', 'v1', $5)
		`, id, orgID, app.ID, status, detectedAt, id)
		if err != nil {
			t.Fatalf("unexpected error seeding opportunity %s: %v", id, err)
		}
	}
	insertOpp("11111111-1111-1111-1111-111111111111", "dismissed", old)
	insertOpp("22222222-2222-2222-2222-222222222222", "stale", recent)
	insertOpp("33333333-3333-3333-3333-333333333333", "open", old)

	retention := NewRetention(pool)
	dropped, err := retention.EnforceOpportunities(context.Background(), orgID, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dropped != 1 {
		t.Errorf("expected exactly 1 dropped (old + dismissed), got %d", dropped)
	}

	var remaining int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM opportunities WHERE org_id = $1`, orgID).Scan(&remaining); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if remaining != 2 {
		t.Errorf("expected 2 remaining opportunities (recent-stale and old-but-open survive), got %d", remaining)
	}
}
