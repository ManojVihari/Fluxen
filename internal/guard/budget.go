package guard

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"

	corepolicy "fluxen/pkg/policy"
	"fluxen/pkg/types"
)

// BudgetGuard tracks and enforces an application's per-period spend via
// a Redis counter keyed by calendar period (Part C.5: "period spend
// counter"). Spend is recorded synchronously right after a request's
// real cost is known — a Redis increment, not a Postgres write, so this
// doesn't violate Part B.2's "never a synchronous DB write on the hot
// path" (the async ingest queue still owns the actual requests-table
// write); it's the same category of hot-path Redis I/O the rate limiter
// and API-key resolver already do.
type BudgetGuard struct {
	redis  *redis.Client
	logger *slog.Logger
}

func NewBudgetGuard(redisClient *redis.Client, logger *slog.Logger) *BudgetGuard {
	if logger == nil {
		logger = slog.Default()
	}
	return &BudgetGuard{redis: redisClient, logger: logger}
}

// Blocked reports whether doc's budget is already exhausted for the
// current period, checked before calling the provider using spend
// recorded by prior requests (V1 has no pre-call cost estimate — see
// pkg/policy.EvaluateBudget's own doc comment for why the check is
// always "already over," never "would go over"). A Redis failure always
// allows (fail-open).
func (g *BudgetGuard) Blocked(ctx context.Context, appID types.AppID, doc *corepolicy.BudgetPolicy) bool {
	if doc == nil || !doc.Enabled {
		return false
	}
	spent, err := g.spend(ctx, appID, doc.Period)
	if err != nil {
		g.logger.Warn("guard: budget check failed, failing open", "app_id", appID, "error", err)
		return false
	}
	return corepolicy.EvaluateBudget(doc, spent)
}

// RecordSpend adds a request's real cost to the current period's
// counter. A zero cost (unknown/local pricing, or a blocked/errored
// request) is a no-op — never worth a round trip. Failure is logged, not
// fatal: the request already completed either way, and losing one
// increment only means the budget counter under-counts slightly rather
// than the request itself failing.
func (g *BudgetGuard) RecordSpend(ctx context.Context, appID types.AppID, period string, costMicro int64) {
	if costMicro <= 0 {
		return
	}
	key := budgetKey(appID, period, time.Now().UTC())
	pipe := g.redis.TxPipeline()
	pipe.IncrBy(ctx, key, costMicro)
	pipe.Expire(ctx, key, periodTTL(period))
	if _, err := pipe.Exec(ctx); err != nil {
		g.logger.Warn("guard: failed to record spend", "app_id", appID, "error", err)
	}
}

func (g *BudgetGuard) spend(ctx context.Context, appID types.AppID, period string) (int64, error) {
	v, err := g.redis.Get(ctx, budgetKey(appID, period, time.Now().UTC())).Int64()
	if err == redis.Nil {
		return 0, nil
	}
	return v, err
}

func budgetKey(appID types.AppID, period string, now time.Time) string {
	return fmt.Sprintf("fluxen:budget:%s:%s:%s", appID, period, periodBucket(period, now))
}

func periodBucket(period string, now time.Time) string {
	if period == corepolicy.BudgetPeriodMonthly {
		return now.Format("200601")
	}
	return now.Format("20060102")
}

func periodTTL(period string) time.Duration {
	if period == corepolicy.BudgetPeriodMonthly {
		return 32 * 24 * time.Hour // safely covers the longest month plus clock skew
	}
	return 25 * time.Hour // safely covers a day plus clock skew
}
