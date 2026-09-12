package policy

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"fluxen/internal/store"
	corepolicy "fluxen/pkg/policy"
	"fluxen/pkg/types"
)

// newTestPool starts a disposable, fully-migrated Postgres container —
// same self-contained pattern every other package's own test suite uses.
func newTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping testcontainers-based policy test in -short mode")
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

func seedApp(t *testing.T, pool *pgxpool.Pool) (types.OrgID, types.AppID) {
	t.Helper()
	orgs := store.NewOrganizations(pool)
	apps := store.NewApplications(pool)

	org, err := orgs.Create(context.Background(), "Acme")
	if err != nil {
		t.Fatalf("failed to seed organization: %v", err)
	}
	app, err := apps.Create(context.Background(), org.ID, "app", "App")
	if err != nil {
		t.Fatalf("failed to seed application: %v", err)
	}
	return org.ID, app.ID
}

func TestStore_GetReturnsEmptyDocumentWhenNoPolicyExists(t *testing.T) {
	pool := newTestPool(t)
	_, appID := seedApp(t, pool)
	s := NewStore(pool)

	rec, err := s.Get(context.Background(), appID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec.Version != 0 {
		t.Errorf("expected version 0 for an app with no policy, got %d", rec.Version)
	}
	if rec.Document.Routing != nil {
		t.Errorf("expected an empty document, got %+v", rec.Document)
	}
}

func TestStore_SaveThenGetRoundTrips(t *testing.T) {
	pool := newTestPool(t)
	_, appID := seedApp(t, pool)
	s := NewStore(pool)

	doc := corepolicy.PolicyDocument{
		Routing: &corepolicy.RoutingPolicy{Enabled: true, FromModel: "gpt-4o", ToModel: "gpt-4o-mini", Weight: 0.5},
	}
	diff, _ := Diff(nil, &doc)

	saved, err := s.Save(context.Background(), appID, 0, doc, HistoryEntry{ChangeSource: "user", Diff: diff})
	if err != nil {
		t.Fatalf("unexpected error saving: %v", err)
	}
	if saved.Version != 1 {
		t.Errorf("expected version 1 after the first save, got %d", saved.Version)
	}
	if saved.Document.Routing == nil || saved.Document.Routing.Weight != 0.5 {
		t.Errorf("expected the saved document to round-trip, got %+v", saved.Document)
	}

	got, err := s.Get(context.Background(), appID)
	if err != nil {
		t.Fatalf("unexpected error getting: %v", err)
	}
	if got.Version != 1 || got.Document.Routing.Weight != 0.5 {
		t.Errorf("expected Get to reflect the saved policy, got %+v", got)
	}
}

func TestStore_SaveRejectsStaleVersion(t *testing.T) {
	pool := newTestPool(t)
	_, appID := seedApp(t, pool)
	s := NewStore(pool)

	doc := corepolicy.PolicyDocument{RateLimit: &corepolicy.RateLimitPolicy{Enabled: true, RequestsPerMinute: 60}}
	if _, err := s.Save(context.Background(), appID, 0, doc, HistoryEntry{ChangeSource: "user"}); err != nil {
		t.Fatalf("unexpected error on first save: %v", err)
	}

	// Trying to save again with the stale expectedVersion=0 must fail —
	// someone else's write (the one above) already landed.
	_, err := s.Save(context.Background(), appID, 0, doc, HistoryEntry{ChangeSource: "user"})
	if err != ErrVersionConflict {
		t.Fatalf("expected ErrVersionConflict, got %v", err)
	}
}

func TestStore_HistoryRecordsEveryVersion(t *testing.T) {
	pool := newTestPool(t)
	_, appID := seedApp(t, pool)
	s := NewStore(pool)

	doc1 := corepolicy.PolicyDocument{RateLimit: &corepolicy.RateLimitPolicy{Enabled: true, RequestsPerMinute: 60}}
	saved1, err := s.Save(context.Background(), appID, 0, doc1, HistoryEntry{ChangeSource: "user", Note: strPtr("turn on rate limiting")})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	doc2 := doc1
	doc2.Budget = &corepolicy.BudgetPolicy{Enabled: true, Period: corepolicy.BudgetPeriodDaily, Mode: corepolicy.BudgetModeHard, LimitMicro: 1_000_000}
	if _, err := s.Save(context.Background(), appID, saved1.Version, doc2, HistoryEntry{ChangeSource: "user"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	history, err := s.History(context.Background(), appID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(history) != 2 {
		t.Fatalf("expected 2 history entries, got %d", len(history))
	}
	if history[0].Version != 2 || history[1].Version != 1 {
		t.Errorf("expected history newest-first (2, 1), got (%d, %d)", history[0].Version, history[1].Version)
	}
	if history[1].Note == nil || *history[1].Note != "turn on rate limiting" {
		t.Errorf("expected the first entry's note to be preserved, got %+v", history[1].Note)
	}
}

func TestStore_AtVersionReturnsHistoricalDocument(t *testing.T) {
	pool := newTestPool(t)
	_, appID := seedApp(t, pool)
	s := NewStore(pool)

	doc1 := corepolicy.PolicyDocument{RateLimit: &corepolicy.RateLimitPolicy{Enabled: true, RequestsPerMinute: 60}}
	saved1, err := s.Save(context.Background(), appID, 0, doc1, HistoryEntry{ChangeSource: "user"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, err := s.AtVersion(context.Background(), appID, saved1.Version)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.RateLimit == nil || got.RateLimit.RequestsPerMinute != 60 {
		t.Errorf("expected the historical document to match version 1, got %+v", got)
	}
}

func strPtr(s string) *string { return &s }
