package rollup

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"fluxen/internal/store"
	"fluxen/pkg/types"
)

// newTestPool starts a disposable, fully-migrated Postgres container. The
// requests table has no foreign keys on org_id/app_id (Part E.1), so
// rollup tests can synthesize arbitrary UUIDs for them without seeding a
// real organization/application first.
func newTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping testcontainers-based rollup test in -short mode")
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

func insertRecord(t *testing.T, reqs *store.Requests, id string, appID types.AppID, startedAt time.Time, provider, model, status string, inputTok, outputTok, durationMS int, costMicro types.Money) {
	t.Helper()
	rec := types.UsageRecord{
		ID: id, OrgID: "99999999-9999-9999-9999-999999999999", AppID: appID, StartedAt: startedAt, DurationMS: durationMS,
		Endpoint: "chat.completions", Protocol: "openai", Streamed: false,
		RequestedModel: model, Provider: provider, Model: model, RouteReason: "direct",
		InputTokens: inputTok, OutputTokens: outputTok, TotalTokens: inputTok + outputTok,
		UsageSource: "provider", Cost: costMicro, CostStatus: types.CostKnown, PricingVersion: "test",
		CacheStatus: "disabled", Status: status, HTTPStatus: 200,
	}
	if err := reqs.InsertBatch(context.Background(), []types.UsageRecord{rec}); err != nil {
		t.Fatalf("failed to insert test record: %v", err)
	}
}

func TestComputeHourly_AggregatesCorrectly(t *testing.T) {
	pool := newTestPool(t)
	reqs := store.NewRequests(pool)

	hour := time.Date(2026, 3, 15, 12, 0, 0, 0, time.UTC)
	appID := types.AppID("11111111-1111-1111-1111-111111111111")

	insertRecord(t, reqs, "00000000-0000-0000-0000-000000000001", appID, hour.Add(5*time.Minute), "openai", "gpt-4o-mini", "ok", 100, 50, 200, 10)
	insertRecord(t, reqs, "00000000-0000-0000-0000-000000000002", appID, hour.Add(40*time.Minute), "openai", "gpt-4o-mini", "ok", 200, 100, 300, 20)
	insertRecord(t, reqs, "00000000-0000-0000-0000-000000000003", appID, hour.Add(10*time.Minute), "openai", "gpt-4o", "provider_error", 50, 0, 100, 0)

	if err := ComputeHourly(context.Background(), pool, hour, hour.Add(time.Hour)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	rows, err := pool.Query(context.Background(), `
		SELECT provider, model, status, requests, input_tokens, output_tokens, cost_micro, duration_ms_sum
		FROM request_rollup_hourly WHERE app_id = $1 ORDER BY model
	`, appID)
	if err != nil {
		t.Fatalf("unexpected error querying rollup: %v", err)
	}
	defer rows.Close()

	type row struct {
		provider, model, status                          string
		requests, inputTok, outputTok, cost, durationSum int64
	}
	var got []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.provider, &r.model, &r.status, &r.requests, &r.inputTok, &r.outputTok, &r.cost, &r.durationSum); err != nil {
			t.Fatalf("unexpected scan error: %v", err)
		}
		got = append(got, r)
	}

	if len(got) != 2 {
		t.Fatalf("expected 2 rollup rows (one per model+status combination), got %d: %+v", len(got), got)
	}

	// gpt-4o (provider_error)
	if got[0].model != "gpt-4o" || got[0].requests != 1 || got[0].inputTok != 50 || got[0].cost != 0 {
		t.Errorf("unexpected gpt-4o rollup row: %+v", got[0])
	}
	// gpt-4o-mini (ok) — two requests merged
	if got[1].model != "gpt-4o-mini" || got[1].requests != 2 || got[1].inputTok != 300 || got[1].outputTok != 150 || got[1].cost != 30 || got[1].durationSum != 500 {
		t.Errorf("unexpected gpt-4o-mini rollup row: %+v", got[1])
	}
}

func TestComputeHourly_IsIdempotent(t *testing.T) {
	pool := newTestPool(t)
	reqs := store.NewRequests(pool)

	hour := time.Date(2026, 3, 15, 12, 0, 0, 0, time.UTC)
	appID := types.AppID("22222222-2222-2222-2222-222222222222")
	insertRecord(t, reqs, "00000000-0000-0000-0000-000000000010", appID, hour.Add(5*time.Minute), "openai", "gpt-4o-mini", "ok", 100, 50, 200, 10)

	window := func() error { return ComputeHourly(context.Background(), pool, hour, hour.Add(time.Hour)) }
	if err := window(); err != nil {
		t.Fatalf("unexpected error on first run: %v", err)
	}
	if err := window(); err != nil {
		t.Fatalf("unexpected error on second run: %v", err)
	}
	if err := window(); err != nil {
		t.Fatalf("unexpected error on third run: %v", err)
	}

	var requests int64
	if err := pool.QueryRow(context.Background(), `SELECT requests FROM request_rollup_hourly WHERE app_id = $1`, appID).Scan(&requests); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if requests != 1 {
		t.Errorf("expected requests=1 after three idempotent runs, got %d (double-counting)", requests)
	}
}

func TestComputeDaily_RollsUpHourlyAndApplicationDaily(t *testing.T) {
	pool := newTestPool(t)
	reqs := store.NewRequests(pool)

	day := time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC)
	appID := types.AppID("33333333-3333-3333-3333-333333333333")

	insertRecord(t, reqs, "00000000-0000-0000-0000-000000000020", appID, day.Add(2*time.Hour), "openai", "gpt-4o-mini", "ok", 100, 50, 200, 10)
	insertRecord(t, reqs, "00000000-0000-0000-0000-000000000021", appID, day.Add(14*time.Hour), "openai", "gpt-4o", "ok", 500, 200, 400, 90)
	insertRecord(t, reqs, "00000000-0000-0000-0000-000000000022", appID, day.Add(20*time.Hour), "openai", "gpt-4o-mini", "timeout", 10, 0, 5000, 0)

	if err := ComputeHourly(context.Background(), pool, day, day.AddDate(0, 0, 1)); err != nil {
		t.Fatalf("unexpected error computing hourly: %v", err)
	}
	if err := ComputeDaily(context.Background(), pool, day, day.AddDate(0, 0, 1)); err != nil {
		t.Fatalf("unexpected error computing daily: %v", err)
	}

	var dailyRows int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM request_rollup_daily WHERE app_id = $1`, appID).Scan(&dailyRows); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dailyRows != 3 {
		t.Errorf("expected 3 daily rollup rows (2 models x mostly-distinct status), got %d", dailyRows)
	}

	var requests, errorsCount, inputTok, cost int64
	err := pool.QueryRow(context.Background(), `
		SELECT requests, errors, input_tokens, cost_micro FROM application_daily WHERE app_id = $1
	`, appID).Scan(&requests, &errorsCount, &inputTok, &cost)
	if err != nil {
		t.Fatalf("unexpected error querying application_daily: %v", err)
	}
	if requests != 3 {
		t.Errorf("expected requests=3, got %d", requests)
	}
	if errorsCount != 1 {
		t.Errorf("expected errors=1 (the timeout), got %d", errorsCount)
	}
	if inputTok != 610 {
		t.Errorf("expected input_tokens=610, got %d", inputTok)
	}
	if cost != 100 {
		t.Errorf("expected cost_micro=100, got %d", cost)
	}
}

func TestComputeDaily_IsIdempotent(t *testing.T) {
	pool := newTestPool(t)
	reqs := store.NewRequests(pool)

	day := time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC)
	appID := types.AppID("44444444-4444-4444-4444-444444444444")
	insertRecord(t, reqs, "00000000-0000-0000-0000-000000000030", appID, day.Add(2*time.Hour), "openai", "gpt-4o-mini", "ok", 100, 50, 200, 10)

	if err := ComputeHourly(context.Background(), pool, day, day.AddDate(0, 0, 1)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	run := func() error { return ComputeDaily(context.Background(), pool, day, day.AddDate(0, 0, 1)) }
	if err := run(); err != nil {
		t.Fatalf("unexpected error on first run: %v", err)
	}
	if err := run(); err != nil {
		t.Fatalf("unexpected error on second run: %v", err)
	}

	var requests int64
	if err := pool.QueryRow(context.Background(), `SELECT requests FROM application_daily WHERE app_id = $1`, appID).Scan(&requests); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if requests != 1 {
		t.Errorf("expected requests=1 after two idempotent daily runs, got %d (double-counting)", requests)
	}
}
