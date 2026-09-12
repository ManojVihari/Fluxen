package guard

import (
	"context"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	tcredis "github.com/testcontainers/testcontainers-go/modules/redis"

	corepolicy "fluxen/pkg/policy"
	"fluxen/pkg/types"
)

// newTestRedis starts a disposable Redis container — same self-contained
// pattern every other package's own test suite uses for its dependency.
func newTestRedis(t *testing.T) *redis.Client {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping testcontainers-based guard test in -short mode")
	}

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

func TestRateLimiter_AllowsUnderLimitBlocksOver(t *testing.T) {
	client := newTestRedis(t)
	rl := NewRateLimiter(client, nil)
	doc := &corepolicy.RateLimitPolicy{Enabled: true, RequestsPerMinute: 3}
	appID := types.AppID("app-1")

	for i := 0; i < 3; i++ {
		if !rl.Allow(context.Background(), appID, doc) {
			t.Fatalf("expected request %d to be allowed under the limit of 3", i+1)
		}
	}
	if rl.Allow(context.Background(), appID, doc) {
		t.Fatal("expected the 4th request to be blocked")
	}
}

func TestRateLimiter_DisabledAlwaysAllows(t *testing.T) {
	client := newTestRedis(t)
	rl := NewRateLimiter(client, nil)
	doc := &corepolicy.RateLimitPolicy{Enabled: false, RequestsPerMinute: 1}

	for i := 0; i < 10; i++ {
		if !rl.Allow(context.Background(), "app-1", doc) {
			t.Fatal("expected a disabled rate limit to never block")
		}
	}
}

func TestRateLimiter_NilRedisClientFailsOpen(t *testing.T) {
	// A client pointed at an address nothing listens on simulates a
	// Redis outage without needing to actually kill a container mid-test.
	client := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1", DialTimeout: 200 * time.Millisecond})
	defer client.Close()

	rl := NewRateLimiter(client, nil)
	doc := &corepolicy.RateLimitPolicy{Enabled: true, RequestsPerMinute: 1}
	if !rl.Allow(context.Background(), "app-1", doc) {
		t.Fatal("expected a rate limiter to fail open when Redis is unreachable")
	}
}

func TestBudgetGuard_BlocksOnceLimitReached(t *testing.T) {
	client := newTestRedis(t)
	bg := NewBudgetGuard(client, nil)
	doc := &corepolicy.BudgetPolicy{Enabled: true, Period: corepolicy.BudgetPeriodDaily, Mode: corepolicy.BudgetModeHard, LimitMicro: 1000}
	appID := types.AppID("app-1")

	if bg.Blocked(context.Background(), appID, doc) {
		t.Fatal("expected no block before any spend is recorded")
	}

	bg.RecordSpend(context.Background(), appID, doc.Period, 999)
	if bg.Blocked(context.Background(), appID, doc) {
		t.Fatal("expected no block just under the limit")
	}

	bg.RecordSpend(context.Background(), appID, doc.Period, 1)
	if !bg.Blocked(context.Background(), appID, doc) {
		t.Fatal("expected a block once spend reaches the limit")
	}
}

func TestBudgetGuard_SoftModeNeverBlocksDespiteRecordedSpend(t *testing.T) {
	client := newTestRedis(t)
	bg := NewBudgetGuard(client, nil)
	doc := &corepolicy.BudgetPolicy{Enabled: true, Period: corepolicy.BudgetPeriodDaily, Mode: corepolicy.BudgetModeSoft, LimitMicro: 100}
	appID := types.AppID("app-1")

	bg.RecordSpend(context.Background(), appID, doc.Period, 10_000)
	if bg.Blocked(context.Background(), appID, doc) {
		t.Fatal("expected soft mode to never block regardless of recorded spend")
	}
}

func TestBudgetGuard_PeriodsAreIsolated(t *testing.T) {
	client := newTestRedis(t)
	bg := NewBudgetGuard(client, nil)
	appID := types.AppID("app-1")

	bg.RecordSpend(context.Background(), appID, corepolicy.BudgetPeriodDaily, 500)
	bg.RecordSpend(context.Background(), appID, corepolicy.BudgetPeriodMonthly, 500)

	dailyDoc := &corepolicy.BudgetPolicy{Enabled: true, Period: corepolicy.BudgetPeriodDaily, Mode: corepolicy.BudgetModeHard, LimitMicro: 500}
	if !bg.Blocked(context.Background(), appID, dailyDoc) {
		t.Error("expected the daily counter to reflect only its own recorded spend")
	}
}

func TestBudgetGuard_NilRedisClientFailsOpen(t *testing.T) {
	client := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1", DialTimeout: 200 * time.Millisecond})
	defer client.Close()

	bg := NewBudgetGuard(client, nil)
	doc := &corepolicy.BudgetPolicy{Enabled: true, Period: corepolicy.BudgetPeriodDaily, Mode: corepolicy.BudgetModeHard, LimitMicro: 1}
	if bg.Blocked(context.Background(), "app-1", doc) {
		t.Fatal("expected a budget guard to fail open when Redis is unreachable")
	}
}
