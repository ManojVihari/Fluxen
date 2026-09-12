package detect

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"fluxen/internal/rollup"
	"fluxen/internal/store"
	"fluxen/pkg/pricing"
	"fluxen/pkg/types"
)

// newTestPool starts a disposable, fully-migrated Postgres container —
// same pattern as internal/store and internal/rollup's own test helpers.
func newTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping testcontainers-based detect test in -short mode")
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

	sqlDB, err := store.OpenSQLDB(dsn)
	if err != nil {
		t.Fatalf("failed to open sql.DB: %v", err)
	}
	if err := store.MigrateUp(sqlDB); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	sqlDB.Close()

	pool, err := store.OpenPool(ctx, dsn)
	if err != nil {
		t.Fatalf("failed to open pool: %v", err)
	}
	t.Cleanup(pool.Close)

	return pool
}

// seedAppWithModelCostTraffic inserts enough gpt-4o/gpt-4o-mini traffic,
// directly via store.Requests, to clear both Part G.2's app-level floors
// and the Model Cost detector's own eligible-fraction floor — the
// minimum fixture that should produce exactly one opportunity end to end.
func seedAppWithModelCostTraffic(t *testing.T, pool *pgxpool.Pool, appID types.AppID, orgID types.OrgID, now time.Time) {
	t.Helper()
	catalog, err := pricing.LoadEmbedded()
	if err != nil {
		t.Fatalf("failed to load catalog: %v", err)
	}
	reqs := store.NewRequests(pool)

	var records []types.UsageRecord
	addRecord := func(model string, inputTokens, outputTokens int, day int) {
		usage := types.ResponseUsage{InputTokens: inputTokens, OutputTokens: outputTokens, TotalTokens: inputTokens + outputTokens}
		costIn, costOut, costTotal, status := pricing.Calculate(catalog, model, usage)
		records = append(records, types.UsageRecord{
			ID: randomUUIDForTest(len(records)), OrgID: orgID, AppID: appID,
			StartedAt: now.AddDate(0, 0, -day), DurationMS: 500,
			Endpoint: "chat.completions", Protocol: "openai", Streamed: false,
			RequestedModel: model, Provider: "openai", Model: model, RouteReason: "direct",
			InputTokens: inputTokens, OutputTokens: outputTokens, TotalTokens: inputTokens + outputTokens,
			UsageSource: "provider",
			Cost:        costTotal, CostInput: costIn, CostOutput: costOut,
			CostStatus: status, PricingVersion: catalog.Version,
			CacheStatus: "disabled", Status: "ok", HTTPStatus: 200,
		})
	}

	// 2200 eligible-shaped gpt-4o requests + 440 genuinely-large ones,
	// spread across the trailing 14 days. The ratio (unlike the pure
	// detector-quality unit fixture, which only needs to clear the
	// eligible-fraction floor) is tuned so the *whole* end-to-end path —
	// including Part G.2's app-level and savings floors, which the unit
	// fixture never exercises — clears every floor with real margin: a
	// genuinely-oversized request costs ~35x an eligible one, so a
	// naive 65/35 split (matching the unit fixture) clears the savings
	// floor but not the savings-percentage floor once the oversized
	// group's cost dominates the application's total spend.
	for i := 0; i < 2200; i++ {
		addRecord("gpt-4o", 1500, 200, i%14)
	}
	for i := 0; i < 440; i++ {
		addRecord("gpt-4o", 90000, 800, i%14)
	}

	if err := reqs.InsertBatch(context.Background(), records); err != nil {
		t.Fatalf("failed to seed traffic: %v", err)
	}
}

// randomUUIDForTest returns a syntactically valid, collision-free-within-
// a-run UUID string keyed by seed — not a real random UUID, just a
// deterministic stand-in the uuid column accepts.
func randomUUIDForTest(seed int) string {
	return fmt.Sprintf("00000000-0000-0000-0000-%012d", seed)
}

func TestRunner_DetectsOpportunity_ReviewLifecycle_AndDedupesOnRerun(t *testing.T) {
	pool := newTestPool(t)
	orgs := store.NewOrganizations(pool)
	apps := store.NewApplications(pool)
	requests := store.NewRequests(pool)
	rollups := store.NewRollups(pool)
	opportunities := store.NewOpportunities(pool)
	catalog, err := pricing.LoadEmbedded()
	if err != nil {
		t.Fatalf("failed to load catalog: %v", err)
	}

	org, err := orgs.Create(context.Background(), "Acme")
	if err != nil {
		t.Fatalf("failed to create org: %v", err)
	}
	app, err := apps.Create(context.Background(), org.ID, "app", "App")
	if err != nil {
		t.Fatalf("failed to create app: %v", err)
	}

	now := time.Now().UTC()
	seedAppWithModelCostTraffic(t, pool, app.ID, org.ID, now)

	// Runner's app-level floor check (Part G.2) reads application_daily,
	// which only rollup.ComputeDaily populates — seeding raw requests
	// alone isn't enough, same as a real deployment waiting on its
	// scheduled rollup job.
	windowStart := now.AddDate(0, 0, -windowDays-1)
	windowEnd := now.AddDate(0, 0, 1)
	if err := rollup.ComputeHourly(context.Background(), pool, windowStart, windowEnd); err != nil {
		t.Fatalf("failed to compute hourly rollups: %v", err)
	}
	if err := rollup.ComputeDaily(context.Background(), pool, windowStart, windowEnd); err != nil {
		t.Fatalf("failed to compute daily rollups: %v", err)
	}

	runner := NewRunner(apps, rollups, requests, opportunities, catalog, nil)
	runner.Now = func() time.Time { return now }

	if err := runner.Run(context.Background()); err != nil {
		t.Fatalf("unexpected error running detectors: %v", err)
	}

	list, err := opportunities.ListByOrg(context.Background(), org.ID, "", "")
	if err != nil {
		t.Fatalf("failed to list opportunities: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected exactly one opportunity, got %d", len(list))
	}
	if list[0].Status != "open" {
		t.Fatalf("expected a freshly-detected opportunity to be 'open', got %q", list[0].Status)
	}

	// open -> reviewed (Part G.1's lifecycle).
	reviewed, err := opportunities.MarkReviewed(context.Background(), org.ID, list[0].ID)
	if err != nil {
		t.Fatalf("failed to mark reviewed: %v", err)
	}
	if reviewed.Status != "reviewed" {
		t.Fatalf("expected status 'reviewed', got %q", reviewed.Status)
	}

	// Re-running the detector against the same, unchanged traffic must
	// not create a duplicate and must not reset the review.
	if err := runner.Run(context.Background()); err != nil {
		t.Fatalf("unexpected error on second run: %v", err)
	}
	list, err = opportunities.ListByOrg(context.Background(), org.ID, "", "")
	if err != nil {
		t.Fatalf("failed to list opportunities after re-run: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected re-running the detector to dedupe, got %d opportunities", len(list))
	}
	if list[0].Status != "reviewed" {
		t.Fatalf("expected the review to survive re-detection, got status %q", list[0].Status)
	}
}

func TestRunner_ApplicationBelowFloorsProducesNothing(t *testing.T) {
	pool := newTestPool(t)
	orgs := store.NewOrganizations(pool)
	apps := store.NewApplications(pool)
	requests := store.NewRequests(pool)
	rollups := store.NewRollups(pool)
	opportunities := store.NewOpportunities(pool)
	catalog, err := pricing.LoadEmbedded()
	if err != nil {
		t.Fatalf("failed to load catalog: %v", err)
	}

	org, err := orgs.Create(context.Background(), "Acme")
	if err != nil {
		t.Fatalf("failed to create org: %v", err)
	}
	if _, err := apps.Create(context.Background(), org.ID, "app", "App"); err != nil {
		t.Fatalf("failed to create app: %v", err)
	}
	// No traffic seeded at all — well below the 1,000 request / $5 floor.

	runner := NewRunner(apps, rollups, requests, opportunities, catalog, nil)
	if err := runner.Run(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	list, err := opportunities.ListByOrg(context.Background(), org.ID, "", "")
	if err != nil {
		t.Fatalf("failed to list opportunities: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected zero opportunities for an application below the traffic/spend floor, got %d", len(list))
	}
}
