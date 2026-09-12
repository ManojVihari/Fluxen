package gateway

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	tcredis "github.com/testcontainers/testcontainers-go/modules/redis"
	"github.com/testcontainers/testcontainers-go/wait"

	"fluxen/internal/auth"
	"fluxen/internal/cache"
	"fluxen/internal/guard"
	"fluxen/internal/ingest"
	"fluxen/internal/policy"
	"fluxen/internal/store"
	corepolicy "fluxen/pkg/policy"
	"fluxen/pkg/providers"
	"fluxen/pkg/types"
)

// newTestPool and newTestRedis start disposable, fully-migrated
// dependencies — the same self-contained pattern every other package's
// own test suite uses.
func newTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping testcontainers-based gateway policy test in -short mode")
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

func newTestRedis(t *testing.T) *redis.Client {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)

	container, err := tcredis.Run(ctx, "redis:7-alpine")
	if err != nil {
		t.Skipf("skipping: could not start redis testcontainer (is Docker running?): %v", err)
	}
	t.Cleanup(func() { _ = container.Terminate(context.Background()) })

	url, err := container.ConnectionString(ctx)
	if err != nil {
		t.Fatalf("failed to get redis connection string: %v", err)
	}
	opts, err := redis.ParseURL(url)
	if err != nil {
		t.Fatalf("failed to parse redis url: %v", err)
	}
	client := redis.NewClient(opts)
	t.Cleanup(func() { _ = client.Close() })
	return client
}

// newPolicyBackedServer wires a full Server against real Postgres/Redis
// for the five controls, with an application already seeded and its
// policy set to doc. It returns the server, the app's own API key, and
// the raw pool (for direct assertions if a test needs them).
func newPolicyBackedServer(t *testing.T, provider providers.Provider, doc corepolicy.PolicyDocument) (*Server, string) {
	t.Helper()
	pool := newTestPool(t)
	redisClient := newTestRedis(t)

	orgs := store.NewOrganizations(pool)
	apps := store.NewApplications(pool)
	keys := store.NewAPIKeys(pool)

	org, err := orgs.Create(context.Background(), "Acme")
	if err != nil {
		t.Fatalf("failed to seed org: %v", err)
	}
	app, err := apps.Create(context.Background(), org.ID, "app", "App")
	if err != nil {
		t.Fatalf("failed to seed app: %v", err)
	}
	rawKey, _, err := keys.Create(context.Background(), app.ID, "test")
	if err != nil {
		t.Fatalf("failed to issue key: %v", err)
	}

	policyStore := policy.NewStore(pool)
	if _, err := policyStore.Save(context.Background(), app.ID, 0, doc, policy.HistoryEntry{ChangeSource: "user"}); err != nil {
		t.Fatalf("failed to seed policy: %v", err)
	}
	snapshot := policy.NewSnapshot(policyStore, redisClient, nil)

	s := NewServer(Server{
		Resolver: auth.NewResolver(keys), Provider: provider, Credential: providers.Credential{APIKey: "sk-test"},
		Catalog: testCatalog(t), Queue: ingest.NewQueue(100, nil),
		Policy:      snapshot,
		RateLimiter: guard.NewRateLimiter(redisClient, nil),
		BudgetGuard: guard.NewBudgetGuard(redisClient, nil),
		Cache:       cache.NewStore(redisClient, nil),
	})
	s.Timeout = 5 * time.Second
	return s, rawKey
}

func echoProvider() providers.Provider {
	return &fakeProvider{
		chatFunc: func(ctx context.Context, req *types.CanonicalRequest, cred providers.Credential) (*types.CanonicalResponse, error) {
			raw := []byte(`{"id":"chatcmpl-1","model":"` + req.Model + `","choices":[{"message":{"role":"assistant","content":"hi"}}]}`)
			return &types.CanonicalResponse{
				ID: "chatcmpl-1", Model: req.Model, Raw: raw,
				Usage: types.ResponseUsage{InputTokens: 10, OutputTokens: 5, TotalTokens: 15},
			}, nil
		},
	}
}

func chatRequest(t *testing.T, ts *httptest.Server, rawKey, model string) *http.Response {
	t.Helper()
	body := `{"model":"` + model + `","messages":[{"role":"user","content":"hi"}]}`
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/v1/chat/completions", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+rawKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return resp
}

func TestGateway_ModelRestriction_BlocksDisallowedModel(t *testing.T) {
	doc := corepolicy.PolicyDocument{ModelRestriction: &corepolicy.ModelRestrictionPolicy{Enabled: true, AllowedModels: []string{"gpt-4o-mini"}}}
	s, rawKey := newPolicyBackedServer(t, echoProvider(), doc)
	ts := httptest.NewServer(s.Router())
	defer ts.Close()

	resp := chatRequest(t, ts, rawKey, "gpt-4o")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 for a disallowed model, got %d", resp.StatusCode)
	}
	if resp.Header.Get("X-Fluxen-Error-Code") != "model_not_allowed" {
		t.Errorf("expected X-Fluxen-Error-Code=model_not_allowed, got %q", resp.Header.Get("X-Fluxen-Error-Code"))
	}

	rec := drainOne(t, s.Queue)
	if rec.Status != "blocked" {
		t.Errorf("expected status=blocked, got %q", rec.Status)
	}

	allowedResp := chatRequest(t, ts, rawKey, "gpt-4o-mini")
	defer allowedResp.Body.Close()
	if allowedResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for an allowed model, got %d", allowedResp.StatusCode)
	}
}

func TestGateway_RateLimit_BlocksOverConfiguredRPM(t *testing.T) {
	doc := corepolicy.PolicyDocument{RateLimit: &corepolicy.RateLimitPolicy{Enabled: true, RequestsPerMinute: 2}}
	s, rawKey := newPolicyBackedServer(t, echoProvider(), doc)
	ts := httptest.NewServer(s.Router())
	defer ts.Close()

	for i := 0; i < 2; i++ {
		resp := chatRequest(t, ts, rawKey, "gpt-4o-mini")
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected request %d to be allowed under the limit, got %d", i+1, resp.StatusCode)
		}
		drainOne(t, s.Queue)
	}

	resp := chatRequest(t, ts, rawKey, "gpt-4o-mini")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected 429 once over the rate limit, got %d", resp.StatusCode)
	}
	rec := drainOne(t, s.Queue)
	if rec.ErrorCode != "rate_limited" {
		t.Errorf("expected error_code=rate_limited, got %q", rec.ErrorCode)
	}
}

func TestGateway_Budget_BlocksOnceLimitReached(t *testing.T) {
	doc := corepolicy.PolicyDocument{Budget: &corepolicy.BudgetPolicy{
		Enabled: true, Period: corepolicy.BudgetPeriodDaily, Mode: corepolicy.BudgetModeHard, LimitMicro: 1,
	}}
	s, rawKey := newPolicyBackedServer(t, echoProvider(), doc)
	ts := httptest.NewServer(s.Router())
	defer ts.Close()

	// First request: spend starts at 0, always allowed regardless of
	// how low the limit is (Part D.2/pkg/policy.EvaluateBudget: checked
	// before, never predicted) — its own cost then pushes spend over.
	first := chatRequest(t, ts, rawKey, "gpt-4o-mini")
	first.Body.Close()
	if first.StatusCode != http.StatusOK {
		t.Fatalf("expected the first request to be allowed, got %d", first.StatusCode)
	}
	drainOne(t, s.Queue)

	// Budget spend is recorded synchronously in afterSuccess before the
	// handler returns, so the second request should already see it.
	second := chatRequest(t, ts, rawKey, "gpt-4o-mini")
	defer second.Body.Close()
	if second.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 once the budget is exhausted, got %d", second.StatusCode)
	}
	rec := drainOne(t, s.Queue)
	if rec.ErrorCode != "budget_exceeded" {
		t.Errorf("expected error_code=budget_exceeded, got %q", rec.ErrorCode)
	}
}

func TestGateway_Routing_SplitConvergesToConfiguredWeight(t *testing.T) {
	doc := corepolicy.PolicyDocument{Routing: &corepolicy.RoutingPolicy{
		Enabled: true, FromModel: "gpt-4o", ToModel: "gpt-4o-mini", Weight: 0.3,
	}}
	s, rawKey := newPolicyBackedServer(t, echoProvider(), doc)
	ts := httptest.NewServer(s.Router())
	defer ts.Close()

	const n = 300
	var routedToCandidate int
	for i := 0; i < n; i++ {
		resp := chatRequest(t, ts, rawKey, "gpt-4o")
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if bytes.Contains(body, []byte(`"model":"gpt-4o-mini"`)) {
			routedToCandidate++
		}
		rec := drainOne(t, s.Queue)
		if rec.RouteReason != "split" {
			t.Fatalf("expected route_reason=split, got %q", rec.RouteReason)
		}
	}

	fraction := float64(routedToCandidate) / n
	if fraction < 0.2 || fraction > 0.4 {
		t.Errorf("expected the observed split to converge near 0.3, got %v (%d/%d)", fraction, routedToCandidate, n)
	}
}

func TestGateway_Caching_SecondIdenticalRequestHitsAndSkipsProvider(t *testing.T) {
	var providerCalls int
	provider := &fakeProvider{
		chatFunc: func(ctx context.Context, req *types.CanonicalRequest, cred providers.Credential) (*types.CanonicalResponse, error) {
			providerCalls++
			raw := []byte(`{"id":"chatcmpl-1","model":"gpt-4o-mini","choices":[{"message":{"role":"assistant","content":"hi"}}]}`)
			return &types.CanonicalResponse{ID: "chatcmpl-1", Model: "gpt-4o-mini", Raw: raw, Usage: types.ResponseUsage{InputTokens: 10, OutputTokens: 5, TotalTokens: 15}}, nil
		},
	}
	doc := corepolicy.PolicyDocument{Caching: &corepolicy.CachingPolicy{Enabled: true, TTLSeconds: 60}}
	s, rawKey := newPolicyBackedServer(t, provider, doc)
	ts := httptest.NewServer(s.Router())
	defer ts.Close()

	first := chatRequest(t, ts, rawKey, "gpt-4o-mini")
	first.Body.Close()
	rec1 := drainOne(t, s.Queue)
	if rec1.CacheStatus != "miss" {
		t.Errorf("expected the first request to be a cache miss, got %q", rec1.CacheStatus)
	}

	second := chatRequest(t, ts, rawKey, "gpt-4o-mini")
	defer second.Body.Close()
	rec2 := drainOne(t, s.Queue)
	if rec2.CacheStatus != "hit" {
		t.Fatalf("expected the second identical request to hit the cache, got %q", rec2.CacheStatus)
	}
	if rec2.Cost != 0 {
		t.Errorf("expected zero real cost on a cache hit, got %d", rec2.Cost)
	}
	if rec2.CacheSavedCost == 0 {
		t.Error("expected a non-zero cache_saved_micro on a hit")
	}
	if providerCalls != 1 {
		t.Fatalf("expected exactly 1 real provider call (the second request must skip it), got %d", providerCalls)
	}
}

func TestGateway_NoPolicy_BehavesLikePhase1(t *testing.T) {
	s, rawKey := newPolicyBackedServer(t, echoProvider(), corepolicy.PolicyDocument{})
	ts := httptest.NewServer(s.Router())
	defer ts.Close()

	resp := chatRequest(t, ts, rawKey, "gpt-4o")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected an empty policy document to behave like Phase 1 (always allowed, direct), got %d", resp.StatusCode)
	}
	rec := drainOne(t, s.Queue)
	if rec.RouteReason != "direct" || rec.CacheStatus != "disabled" {
		t.Errorf("expected route_reason=direct and cache_status=disabled with no policy, got %+v", rec)
	}
}
