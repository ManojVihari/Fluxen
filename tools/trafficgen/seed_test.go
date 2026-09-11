package trafficgen

import (
	"context"
	"math/rand"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"fluxen/internal/store"
)

func newTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping testcontainers-based trafficgen test in -short mode")
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

func demoOpts() Options {
	return Options{
		OrgName: "Acme", OwnerEmail: "owner@example.com", OwnerPasswd: "supersecret123",
		AppName: "Document AI", Days: 5,
		Now: time.Now().UTC(), Rand: rand.New(rand.NewSource(7)),
	}
}

func TestSeed_CreatesOrgAppKeyAndTraffic(t *testing.T) {
	pool := newTestPool(t)

	result, err := Seed(context.Background(), pool, demoOpts())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.AppSlug != "document-ai" {
		t.Errorf("expected slug 'document-ai', got %q", result.AppSlug)
	}
	if result.RequestsGenerated == 0 {
		t.Fatal("expected requests to be generated")
	}
	if result.AlreadySeeded {
		t.Error("expected AlreadySeeded=false on the first run")
	}

	var appCount, keyCount, requestCount int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM applications WHERE id = $1`, result.AppID).Scan(&appCount); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if appCount != 1 {
		t.Errorf("expected the application to exist, got count=%d", appCount)
	}
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM api_keys WHERE app_id = $1`, result.AppID).Scan(&keyCount); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if keyCount != 1 {
		t.Errorf("expected one api key to be issued, got %d", keyCount)
	}
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM requests WHERE app_id = $1`, result.AppID).Scan(&requestCount); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if requestCount != result.RequestsGenerated {
		t.Errorf("expected %d rows in requests, got %d", result.RequestsGenerated, requestCount)
	}

	// Rollups were computed as part of Seed — application_daily should
	// already reflect the seeded traffic without a separate job run.
	var dailyRows int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM application_daily WHERE app_id = $1`, result.AppID).Scan(&dailyRows); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dailyRows == 0 {
		t.Error("expected Seed to have computed application_daily rollups for the generated traffic")
	}
}

func TestSeed_IsIdempotent(t *testing.T) {
	pool := newTestPool(t)
	opts := demoOpts()

	first, err := Seed(context.Background(), pool, opts)
	if err != nil {
		t.Fatalf("unexpected error on first run: %v", err)
	}

	second, err := Seed(context.Background(), pool, opts)
	if err != nil {
		t.Fatalf("unexpected error on second run: %v", err)
	}
	if !second.AlreadySeeded {
		t.Error("expected the second run to report AlreadySeeded=true")
	}
	if second.AppID != first.AppID {
		t.Error("expected the second run to reuse the same application")
	}

	var requestCount int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM requests WHERE app_id = $1`, first.AppID).Scan(&requestCount); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if requestCount != first.RequestsGenerated {
		t.Errorf("expected re-running seed not to duplicate traffic: still %d requests, got %d", first.RequestsGenerated, requestCount)
	}
}

func TestSeed_ReusesExistingOrganization(t *testing.T) {
	pool := newTestPool(t)
	orgs := store.NewOrganizations(pool)

	pre, err := orgs.Create(context.Background(), "Pre-existing Org")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result, err := Seed(context.Background(), pool, demoOpts())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.OrgID != pre.ID {
		t.Errorf("expected Seed to reuse the existing organization %v, got %v", pre.ID, result.OrgID)
	}
}
