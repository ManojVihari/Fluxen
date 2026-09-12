package store

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"fluxen/pkg/types"
)

func TestRequests_ListAndGet(t *testing.T) {
	pool := newTestPool(t)
	requests := NewRequests(pool)
	orgID := seedOrg(t, pool)
	appID := types.AppID(uuid.NewString())

	base := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	ids := make([]string, 3)
	for i := 0; i < 3; i++ {
		ids[i] = uuid.NewString()
		_, err := pool.Exec(context.Background(), `
			INSERT INTO requests (
				id, org_id, app_id, started_at, duration_ms,
				endpoint, protocol, streamed,
				requested_model, provider, model,
				input_tokens, output_tokens, total_tokens,
				cost_micro, cost_status, cache_status, status, http_status
			) VALUES ($1, $2, $3, $4, 100, 'chat.completions', 'openai', false, 'gpt-4o-mini', 'openai', 'gpt-4o-mini', 10, 5, 15, 1000, 'known', 'miss', 'ok', 200)
		`, ids[i], orgID, appID, base.Add(time.Duration(i)*time.Minute))
		if err != nil {
			t.Fatalf("unexpected error seeding request %d: %v", i, err)
		}
	}

	rows, next, err := requests.List(context.Background(), orgID, RequestsFilter{AppID: appID, Limit: 2})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows on the first page, got %d", len(rows))
	}
	// Newest first: index 2 (base+2m), then index 1 (base+1m).
	if rows[0].ID != ids[2] || rows[1].ID != ids[1] {
		t.Errorf("expected newest-first order [%s,%s], got [%s,%s]", ids[2], ids[1], rows[0].ID, rows[1].ID)
	}
	if next == "" {
		t.Fatal("expected a next cursor since a third row remains")
	}

	rows2, next2, err := requests.List(context.Background(), orgID, RequestsFilter{AppID: appID, Limit: 2, Cursor: next})
	if err != nil {
		t.Fatalf("unexpected error on page 2: %v", err)
	}
	if len(rows2) != 1 || rows2[0].ID != ids[0] {
		t.Fatalf("expected the last remaining row (oldest) on page 2, got %+v", rows2)
	}
	if next2 != "" {
		t.Errorf("expected no further cursor once every row has been returned, got %q", next2)
	}

	detail, err := requests.Get(context.Background(), orgID, ids[0])
	if err != nil {
		t.Fatalf("unexpected error fetching detail: %v", err)
	}
	if detail.Model != "gpt-4o-mini" || detail.CostMicro != 1000 || detail.Status != "ok" {
		t.Errorf("unexpected detail row: %+v", detail)
	}

	if _, err := requests.Get(context.Background(), orgID, uuid.NewString()); err != ErrNotFound {
		t.Errorf("expected ErrNotFound for an unknown id, got %v", err)
	}
}

func TestRequests_ListFiltersByStatusAndModel(t *testing.T) {
	pool := newTestPool(t)
	requests := NewRequests(pool)
	orgID := seedOrg(t, pool)
	appID := types.AppID(uuid.NewString())
	base := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

	_, err := pool.Exec(context.Background(), `
		INSERT INTO requests (id, org_id, app_id, started_at, duration_ms, endpoint, protocol, streamed, requested_model, provider, model, status, cost_status, cache_status)
		VALUES
			($1, $3, $4, $5, 100, 'chat.completions', 'openai', false, 'gpt-4o', 'openai', 'gpt-4o', 'ok', 'known', 'miss'),
			($2, $3, $4, $5, 100, 'chat.completions', 'openai', false, 'gpt-4o-mini', 'openai', 'gpt-4o-mini', 'provider_error', 'unknown', 'disabled')
	`, uuid.NewString(), uuid.NewString(), orgID, appID, base)
	if err != nil {
		t.Fatalf("unexpected error seeding requests: %v", err)
	}

	rows, _, err := requests.List(context.Background(), orgID, RequestsFilter{AppID: appID, Status: "ok"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 1 || rows[0].Model != "gpt-4o" {
		t.Fatalf("expected exactly one ok row for gpt-4o, got %+v", rows)
	}

	rows, _, err = requests.List(context.Background(), orgID, RequestsFilter{AppID: appID, Model: "gpt-4o-mini"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 1 || rows[0].Status != "provider_error" {
		t.Fatalf("expected exactly one gpt-4o-mini row, got %+v", rows)
	}
}
