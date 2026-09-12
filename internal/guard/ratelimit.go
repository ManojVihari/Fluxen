// Package guard is the gateway's live, Redis-backed enforcement for the
// two stateful controls — rate limit and budget (PRD §22) — that can't
// be pure decisions (pkg/policy.Evaluate) because they depend on a
// running counter, not just the request in front of them. Both fail
// open: any Redis error allows the request through rather than turning
// an infrastructure hiccup in a secondary system into an outage for the
// customer's real traffic (Part L Phase 5 backend tasks, stated twice:
// "fail-open if Redis unreachable").
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

// rateLimitScript atomically increments the current minute's counter and
// sets its expiry only on the first increment — INCR and EXPIRE must be
// one atomic op, or a crash between them leaves a key with no TTL, a
// slow permanent leak.
var rateLimitScript = redis.NewScript(`
local count = redis.call("INCR", KEYS[1])
if count == 1 then
    redis.call("EXPIRE", KEYS[1], ARGV[1])
end
return count
`)

// RateLimiter enforces RateLimitPolicy via a Redis-backed fixed-window
// counter, one window per calendar minute — simpler than a true token
// bucket and sufficient for "N requests per minute" (PRD §22's own
// wording).
type RateLimiter struct {
	redis  *redis.Client
	logger *slog.Logger
}

func NewRateLimiter(redisClient *redis.Client, logger *slog.Logger) *RateLimiter {
	if logger == nil {
		logger = slog.Default()
	}
	return &RateLimiter{redis: redisClient, logger: logger}
}

// Allow reports whether a request should proceed under doc's rate limit.
// A nil or disabled policy always allows; a Redis failure always allows
// (fail-open).
func (g *RateLimiter) Allow(ctx context.Context, appID types.AppID, doc *corepolicy.RateLimitPolicy) bool {
	if doc == nil || !doc.Enabled {
		return true
	}
	key := fmt.Sprintf("fluxen:ratelimit:%s:%s", appID, time.Now().UTC().Format("200601021504"))
	count, err := rateLimitScript.Run(ctx, g.redis, []string{key}, 60).Int64()
	if err != nil {
		g.logger.Warn("guard: rate limit check failed, failing open", "app_id", appID, "error", err)
		return true
	}
	return count <= int64(doc.RequestsPerMinute)
}
