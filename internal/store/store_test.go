package store

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"fluxen/pkg/types"
)

// newTestPool starts a disposable Postgres container, applies every
// migration, and returns a connected pool. Each store_test.go test gets
// its own container — cheap enough given Phase 1's schema size, and it
// means tests never leak state into each other. Skips (not fails) when
// Docker isn't available, matching cmd/fluxen's boot smoke test.
func newTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping testcontainers-based store test in -short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	t.Cleanup(cancel)

	pgContainer, err := tcpostgres.Run(ctx, "postgres:16-alpine",
		tcpostgres.WithDatabase("fluxen"),
		tcpostgres.WithUsername("fluxen"),
		tcpostgres.WithPassword("fluxen"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").WithOccurrence(2).WithStartupTimeout(60*time.Second),
		),
	)
	if err != nil {
		t.Skipf("skipping: could not start postgres testcontainer (is Docker running?): %v", err)
	}
	t.Cleanup(func() { _ = pgContainer.Terminate(context.Background()) })

	dsn, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("failed to get connection string: %v", err)
	}

	sqlDB, err := OpenSQLDB(dsn)
	if err != nil {
		t.Fatalf("failed to open sql.DB: %v", err)
	}
	if err := MigrateUp(sqlDB); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	sqlDB.Close()

	pool, err := OpenPool(ctx, dsn)
	if err != nil {
		t.Fatalf("failed to open pool: %v", err)
	}
	t.Cleanup(pool.Close)

	return pool
}

func seedOrg(t *testing.T, pool *pgxpool.Pool) types.OrgID {
	t.Helper()
	orgs := NewOrganizations(pool)
	org, err := orgs.Create(context.Background(), "Acme")
	if err != nil {
		t.Fatalf("failed to seed organization: %v", err)
	}
	return org.ID
}

func TestApplications_CreateListGet(t *testing.T) {
	pool := newTestPool(t)
	orgID := seedOrg(t, pool)
	apps := NewApplications(pool)

	created, err := apps.Create(context.Background(), orgID, "document-ai", "Document AI")
	if err != nil {
		t.Fatalf("unexpected error creating application: %v", err)
	}
	if created.Status != "active" {
		t.Errorf("expected default status 'active', got %q", created.Status)
	}

	list, err := apps.ListByOrg(context.Background(), orgID)
	if err != nil {
		t.Fatalf("unexpected error listing applications: %v", err)
	}
	if len(list) != 1 || list[0].ID != created.ID {
		t.Fatalf("expected the created application in the list, got %+v", list)
	}

	got, err := apps.Get(context.Background(), orgID, created.ID)
	if err != nil {
		t.Fatalf("unexpected error getting application: %v", err)
	}
	if got.Slug != "document-ai" {
		t.Errorf("expected slug 'document-ai', got %q", got.Slug)
	}
}

func TestApplications_GetScopedToOrg(t *testing.T) {
	pool := newTestPool(t)
	org1 := seedOrg(t, pool)
	apps := NewApplications(pool)

	created, err := apps.Create(context.Background(), org1, "app-one", "App One")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	otherOrg := types.OrgID("00000000-0000-0000-0000-000000000000")
	_, err = apps.Get(context.Background(), otherOrg, created.ID)
	if err != ErrNotFound {
		t.Fatalf("expected ErrNotFound when fetching an application under the wrong org, got %v", err)
	}
}

func TestApplications_UniqueSlugDisambiguates(t *testing.T) {
	pool := newTestPool(t)
	orgID := seedOrg(t, pool)
	apps := NewApplications(pool)

	slug1, err := apps.UniqueSlug(context.Background(), orgID, "Document AI")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := apps.Create(context.Background(), orgID, slug1, "Document AI"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	slug2, err := apps.UniqueSlug(context.Background(), orgID, "Document AI")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if slug1 == slug2 {
		t.Fatalf("expected a disambiguated slug for a second application with the same name, got %q twice", slug1)
	}
}

func TestAPIKeys_CreateAndLookup(t *testing.T) {
	pool := newTestPool(t)
	orgID := seedOrg(t, pool)
	apps := NewApplications(pool)
	keys := NewAPIKeys(pool)

	app, err := apps.Create(context.Background(), orgID, "app", "App")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	raw, key, err := keys.Create(context.Background(), app.ID, "default")
	if err != nil {
		t.Fatalf("unexpected error creating key: %v", err)
	}
	if raw == "" {
		t.Fatal("expected a non-empty raw key")
	}

	rec, ok, err := keys.LookupAPIKeyByPrefix(context.Background(), key.Prefix)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected the key's prefix to resolve")
	}
	if rec.AppID != app.ID {
		t.Errorf("expected AppID=%v, got %v", app.ID, rec.AppID)
	}
	if rec.OrgID != orgID {
		t.Errorf("expected OrgID=%v, got %v", orgID, rec.OrgID)
	}
	if rec.Revoked {
		t.Error("expected a freshly-created key to not be revoked")
	}
}

func TestAPIKeys_RevokeTakesEffect(t *testing.T) {
	pool := newTestPool(t)
	orgID := seedOrg(t, pool)
	apps := NewApplications(pool)
	keys := NewAPIKeys(pool)

	app, err := apps.Create(context.Background(), orgID, "app", "App")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_, key, err := keys.Create(context.Background(), app.ID, "default")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	revokedPrefix, err := keys.Revoke(context.Background(), orgID, key.ID)
	if err != nil {
		t.Fatalf("unexpected error revoking key: %v", err)
	}
	if revokedPrefix != key.Prefix {
		t.Errorf("expected Revoke to return the key's prefix %q, got %q", key.Prefix, revokedPrefix)
	}

	rec, ok, err := keys.LookupAPIKeyByPrefix(context.Background(), key.Prefix)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected the revoked key's prefix to still resolve to a record")
	}
	if !rec.Revoked {
		t.Error("expected the key to be marked revoked")
	}
}

func TestAPIKeys_RevokeWrongOrgFails(t *testing.T) {
	pool := newTestPool(t)
	orgID := seedOrg(t, pool)
	apps := NewApplications(pool)
	keys := NewAPIKeys(pool)

	app, err := apps.Create(context.Background(), orgID, "app", "App")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_, key, err := keys.Create(context.Background(), app.ID, "default")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	otherOrg := types.OrgID("00000000-0000-0000-0000-000000000000")
	_, err = keys.Revoke(context.Background(), otherOrg, key.ID)
	if err != ErrNotFound {
		t.Fatalf("expected ErrNotFound when revoking a key under the wrong org, got %v", err)
	}
}

func TestUsers_CreateAndGetByEmail(t *testing.T) {
	pool := newTestPool(t)
	orgID := seedOrg(t, pool)
	users := NewUsers(pool)

	created, err := users.Create(context.Background(), orgID, "owner@example.com", "hashed", "owner")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, err := users.GetByEmail(context.Background(), "owner@example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ID != created.ID {
		t.Errorf("expected to get back the same user by email")
	}
}

func TestRequests_InsertBatch(t *testing.T) {
	pool := newTestPool(t)
	orgID := seedOrg(t, pool)
	apps := NewApplications(pool)
	reqs := NewRequests(pool)

	app, err := apps.Create(context.Background(), orgID, "app", "App")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	records := []types.UsageRecord{
		{
			ID: "11111111-1111-1111-1111-111111111111", OrgID: orgID, AppID: app.ID,
			StartedAt: time.Now(), DurationMS: 120,
			Endpoint: "chat.completions", Protocol: "openai", Streamed: false,
			RequestedModel: "gpt-4o-mini", Provider: "openai", Model: "gpt-4o-mini",
			RouteReason: "direct", InputTokens: 10, OutputTokens: 5, TotalTokens: 15,
			UsageSource: "provider", Cost: 100, CostInput: 60, CostOutput: 40,
			CostStatus: types.CostKnown, PricingVersion: "test", CacheStatus: "disabled",
			Status: "ok", HTTPStatus: 200,
		},
		{
			ID: "22222222-2222-2222-2222-222222222222", OrgID: orgID, AppID: app.ID,
			StartedAt: time.Now(), DurationMS: 80,
			Endpoint: "chat.completions", Protocol: "openai", Streamed: true,
			RequestedModel: "gpt-4o-mini", Provider: "openai", Model: "gpt-4o-mini",
			RouteReason: "direct", InputTokens: 8, OutputTokens: 2, TotalTokens: 10,
			UsageSource: "provider", Cost: 40, CostStatus: types.CostKnown,
			PricingVersion: "test", CacheStatus: "disabled", Status: "ok", HTTPStatus: 200,
		},
	}

	if err := reqs.InsertBatch(context.Background(), records); err != nil {
		t.Fatalf("unexpected error inserting batch: %v", err)
	}

	var count int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM requests WHERE app_id = $1`, app.ID).Scan(&count); err != nil {
		t.Fatalf("unexpected error counting requests: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 requests to be inserted, got %d", count)
	}
}

// TestRequests_InsertBatch_CapturesBodyWhenSet is a regression test for
// Part G.6's opt-in body capture: request_body/response_body must round-
// trip through InsertBatch -> Get exactly, and stay nil when a record
// carries no captured bytes (the off-by-default case).
func TestRequests_InsertBatch_CapturesBodyWhenSet(t *testing.T) {
	pool := newTestPool(t)
	orgID := seedOrg(t, pool)
	apps := NewApplications(pool)
	reqs := NewRequests(pool)

	app, err := apps.Create(context.Background(), orgID, "app", "App")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	captured := types.UsageRecord{
		ID: "33333333-3333-3333-3333-333333333333", OrgID: orgID, AppID: app.ID,
		StartedAt: time.Now(), DurationMS: 50,
		Endpoint: "chat.completions", Protocol: "openai", Streamed: false,
		RequestedModel: "gpt-4o-mini", Provider: "openai", Model: "gpt-4o-mini",
		RouteReason: "direct", InputTokens: 5, OutputTokens: 3, TotalTokens: 8,
		UsageSource: "provider", Cost: 10, CostStatus: types.CostKnown,
		PricingVersion: "test", CacheStatus: "disabled", Status: "ok", HTTPStatus: 200,
		RequestBody:  []byte(`{"model":"gpt-4o-mini","messages":[{"role":"user","content":"hi"}]}`),
		ResponseBody: []byte(`{"id":"chatcmpl-1","choices":[{"message":{"role":"assistant","content":"hi"}}]}`),
	}
	notCaptured := captured
	notCaptured.ID = "44444444-4444-4444-4444-444444444444"
	notCaptured.RequestBody = nil
	notCaptured.ResponseBody = nil

	if err := reqs.InsertBatch(context.Background(), []types.UsageRecord{captured, notCaptured}); err != nil {
		t.Fatalf("unexpected error inserting batch: %v", err)
	}

	got, err := reqs.Get(context.Background(), orgID, captured.ID)
	if err != nil {
		t.Fatalf("unexpected error fetching captured record: %v", err)
	}
	// jsonb re-serializes on storage (whitespace/key-order aren't
	// preserved byte-for-byte) — compare parsed equality, not raw bytes.
	assertJSONEqual(t, "request_body", captured.RequestBody, got.RequestBody)
	assertJSONEqual(t, "response_body", captured.ResponseBody, got.ResponseBody)

	gotUncaptured, err := reqs.Get(context.Background(), orgID, notCaptured.ID)
	if err != nil {
		t.Fatalf("unexpected error fetching uncaptured record: %v", err)
	}
	if gotUncaptured.RequestBody != nil || gotUncaptured.ResponseBody != nil {
		t.Errorf("expected no body for a record that never captured one, got request=%q response=%q", gotUncaptured.RequestBody, gotUncaptured.ResponseBody)
	}
}

func assertJSONEqual(t *testing.T, field string, want, got []byte) {
	t.Helper()
	var wantVal, gotVal any
	if err := json.Unmarshal(want, &wantVal); err != nil {
		t.Fatalf("unexpected error parsing expected %s: %v", field, err)
	}
	if err := json.Unmarshal(got, &gotVal); err != nil {
		t.Fatalf("unexpected error parsing stored %s (%q): %v", field, got, err)
	}
	if !reflect.DeepEqual(wantVal, gotVal) {
		t.Errorf("expected %s to round-trip (parsed) equal, want %s got %s", field, want, got)
	}
}

func TestRequests_InsertBatch_Empty(t *testing.T) {
	pool := newTestPool(t)
	reqs := NewRequests(pool)
	if err := reqs.InsertBatch(context.Background(), nil); err != nil {
		t.Fatalf("expected InsertBatch(nil) to be a no-op, got error: %v", err)
	}
}
