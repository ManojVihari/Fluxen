package store

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func newTestOpportunity() Opportunity {
	return Opportunity{
		Kind: "model_cost", Fingerprint: "fp-1",
		Title: "Title", Summary: "Summary",
		WindowStart: time.Now().Add(-14 * 24 * time.Hour), WindowEnd: time.Now(), SampleRequests: 2000,
		CurrentCostMicro: 100_000_000, ProjectedCostMicro: 90_000_000, SavingsMicro: 10_000_000, SavingsPct: 0.1,
		Confidence: "medium", ConfidenceScore: 0.6,
		Evidence:        json.RawMessage(`{"a":1}`),
		Recommendation:  json.RawMessage(`{"b":2}`),
		DetectorVersion: "test-1",
	}
}

func TestOpportunities_UpsertOpen_CreatesThenRefreshesWithoutDuplicating(t *testing.T) {
	pool := newTestPool(t)
	orgID := seedOrg(t, pool)
	apps := NewApplications(pool)
	opps := NewOpportunities(pool)

	app, err := apps.Create(context.Background(), orgID, "app", "App")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	in := newTestOpportunity()
	in.OrgID = orgID
	in.AppID = app.ID

	first, err := opps.UpsertOpen(context.Background(), in)
	if err != nil {
		t.Fatalf("unexpected error on first upsert: %v", err)
	}
	if first.Status != "open" {
		t.Errorf("expected a freshly-detected opportunity to start 'open', got %q", first.Status)
	}

	// Re-running the detector against unchanged traffic must not create
	// a duplicate — same (app_id, fingerprint) should update in place.
	in.SavingsMicro = 12_000_000
	second, err := opps.UpsertOpen(context.Background(), in)
	if err != nil {
		t.Fatalf("unexpected error on second upsert: %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("expected the second upsert to update the same row, got a different id (%s != %s)", second.ID, first.ID)
	}
	if second.SavingsMicro != 12_000_000 {
		t.Errorf("expected refreshed savings, got %d", second.SavingsMicro)
	}

	list, err := opps.ListByOrg(context.Background(), orgID, "", "")
	if err != nil {
		t.Fatalf("unexpected error listing: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected exactly one opportunity after re-detection, got %d", len(list))
	}
}

func TestOpportunities_MarkReviewed_TransitionsOpenToReviewedAndIsIdempotent(t *testing.T) {
	pool := newTestPool(t)
	orgID := seedOrg(t, pool)
	apps := NewApplications(pool)
	opps := NewOpportunities(pool)

	app, err := apps.Create(context.Background(), orgID, "app", "App")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	in := newTestOpportunity()
	in.OrgID = orgID
	in.AppID = app.ID
	created, err := opps.UpsertOpen(context.Background(), in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	reviewed, err := opps.MarkReviewed(context.Background(), orgID, created.ID)
	if err != nil {
		t.Fatalf("unexpected error marking reviewed: %v", err)
	}
	if reviewed.Status != "reviewed" {
		t.Fatalf("expected status 'reviewed', got %q", reviewed.Status)
	}
	if reviewed.ReviewedAt == nil {
		t.Fatal("expected reviewed_at to be set")
	}

	// Calling it again must be a no-op, not an error.
	again, err := opps.MarkReviewed(context.Background(), orgID, created.ID)
	if err != nil {
		t.Fatalf("unexpected error on repeat review: %v", err)
	}
	if again.Status != "reviewed" {
		t.Fatalf("expected status to remain 'reviewed', got %q", again.Status)
	}
}

func TestOpportunities_GetScopedToOrg(t *testing.T) {
	pool := newTestPool(t)
	orgID := seedOrg(t, pool)
	apps := NewApplications(pool)
	opps := NewOpportunities(pool)

	app, err := apps.Create(context.Background(), orgID, "app", "App")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	in := newTestOpportunity()
	in.OrgID = orgID
	in.AppID = app.ID
	created, err := opps.UpsertOpen(context.Background(), in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	otherOrg := seedOrg(t, pool)
	if _, err := opps.Get(context.Background(), otherOrg, created.ID); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound fetching an opportunity under the wrong org, got %v", err)
	}
}
