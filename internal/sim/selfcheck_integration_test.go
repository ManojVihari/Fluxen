package sim

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"fluxen/internal/store"
	"fluxen/pkg/pricing"
	"fluxen/pkg/types"
)

// newTestPool starts a disposable, fully-migrated Postgres container —
// same self-contained pattern internal/rollup and internal/detect each
// keep their own copy of (Part K: "sim_test.go" is this suite).
func newTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping testcontainers-based sim test in -short mode")
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

// TestSelfCheck_ReplayReproducesRecordedCostAgainstRealPostgres is Part
// G.4/K's release-blocking determinism check: replaying real historical
// requests must reproduce actual recorded cost within 0.5% (Part G.4).
// This is the credibility floor for the entire simulation feature —
// CI failing this test blocks the release (Part K).
func TestSelfCheck_ReplayReproducesRecordedCostAgainstRealPostgres(t *testing.T) {
	pool := newTestPool(t)
	catalog, err := pricing.LoadEmbedded()
	if err != nil {
		t.Fatalf("failed to load catalog: %v", err)
	}

	orgID := types.OrgID("11111111-1111-1111-1111-111111111111")
	appID := types.AppID("22222222-2222-2222-2222-222222222222")

	models := []struct {
		name                      string
		inputTokens, outputTokens int
	}{
		{"gpt-4o", 1500, 200},
		{"gpt-4o", 60000, 900},
		{"gpt-4o-mini", 2000, 150},
		{"gpt-3.5-turbo", 800, 100},
		{"gpt-4-turbo", 12000, 600},
	}

	now := time.Now().UTC()
	var records []types.UsageRecord
	id := 0
	for _, m := range models {
		for i := 0; i < 50; i++ {
			id++
			usage := types.ResponseUsage{InputTokens: m.inputTokens, OutputTokens: m.outputTokens, TotalTokens: m.inputTokens + m.outputTokens}
			_, _, total, status := pricing.Calculate(catalog, m.name, usage)
			records = append(records, types.UsageRecord{
				ID: fmt.Sprintf("00000000-0000-0000-0000-%012d", id), OrgID: orgID, AppID: appID,
				StartedAt: now.Add(-time.Duration(id) * time.Minute), DurationMS: 500,
				Endpoint: "chat.completions", Protocol: "openai", Streamed: false,
				RequestedModel: m.name, Provider: "openai", Model: m.name, RouteReason: "direct",
				InputTokens: m.inputTokens, OutputTokens: m.outputTokens, TotalTokens: m.inputTokens + m.outputTokens,
				UsageSource: "provider",
				Cost:        total, CostStatus: status, PricingVersion: catalog.Version,
				CacheStatus: "disabled", Status: "ok", HTTPStatus: 200,
			})
		}
	}
	// A handful of error requests with zero usage — these must also
	// reproduce (zero actual, zero replayed), not skew the check.
	for i := 0; i < 5; i++ {
		id++
		records = append(records, types.UsageRecord{
			ID: fmt.Sprintf("00000000-0000-0000-0000-%012d", id), OrgID: orgID, AppID: appID,
			StartedAt: now.Add(-time.Duration(id) * time.Minute), DurationMS: 100,
			Endpoint: "chat.completions", Protocol: "openai", Streamed: false,
			RequestedModel: "gpt-4o", Provider: "openai", Model: "gpt-4o", RouteReason: "direct",
			UsageSource: "provider", CostStatus: types.CostKnown,
			CacheStatus: "disabled", Status: "provider_error", HTTPStatus: 500, ErrorCode: "provider_error",
		})
	}

	reqs := store.NewRequests(pool)
	if err := reqs.InsertBatch(context.Background(), records); err != nil {
		t.Fatalf("failed to seed requests: %v", err)
	}

	windowStart := now.Add(-24 * time.Hour)
	windowEnd := now.Add(time.Hour)
	storeFacts, err := reqs.ReplayFacts(context.Background(), appID, windowStart, windowEnd)
	if err != nil {
		t.Fatalf("failed to load replay facts: %v", err)
	}
	if len(storeFacts) != len(records) {
		t.Fatalf("expected to replay all %d seeded requests, got %d", len(records), len(storeFacts))
	}

	facts := make([]ReplayFact, len(storeFacts))
	for i, f := range storeFacts {
		facts[i] = ReplayFact{
			ID: f.ID, StartedAt: f.StartedAt, RequestedModel: f.RequestedModel, Provider: f.Provider, Model: f.Model,
			InputTokens: f.InputTokens, OutputTokens: f.OutputTokens, CostMicro: f.CostMicro, CostStatus: f.CostStatus, Status: f.Status,
		}
	}

	result := SelfCheck(facts, catalog)
	if !result.Pass {
		t.Fatalf("RELEASE-BLOCKING: replay failed to reproduce recorded cost within %.2f%% (got %.4f%%, actual=%d replayed=%d)",
			SelfCheckThresholdPct*100, result.DeltaPct*100, result.ActualCostMicro, result.ReplayedCostMicro)
	}
	if result.ActualCostMicro != result.ReplayedCostMicro {
		t.Errorf("expected exact reproduction against real Postgres-stored data, got actual=%d replayed=%d", result.ActualCostMicro, result.ReplayedCostMicro)
	}
}
