package auth

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
)

// loginRateLimitScript is the same atomic increment-and-expire pattern as
// internal/guard's gateway rate limiter, duplicated in miniature rather
// than imported — guard is gateway-policy-scoped (keyed by application,
// driven by a customer-editable RateLimitPolicy) and pulling it in here
// for a two-line script would couple an auth concern to gateway policy
// for no real reuse.
var loginRateLimitScript = redis.NewScript(`
local count = redis.call("INCR", KEYS[1])
if count == 1 then
    redis.call("EXPIRE", KEYS[1], ARGV[1])
end
return count
`)

const (
	loginRateLimitMaxAttempts = 10
	loginRateLimitWindow      = 5 * time.Minute
)

// LoginLimiter throttles login attempts per source IP to blunt
// brute-force/credential-stuffing against the dashboard's password login
// (Part L Phase 9's own admitted gap). A fixed window keyed by IP, not by
// the attempted email — this also protects against attempts targeting
// unknown/enumerated emails, not just real ones.
//
// Fails open on a Redis error: consistent with the rest of Fluxen's
// stated policy that an infra hiccup in a secondary system never turns
// into a real outage (guard.RateLimiter's own doc comment says the same
// for gateway traffic) — accepted here as the tradeoff for a self-hosted
// product with no separate WAF/edge layer in front of it.
type LoginLimiter struct {
	redis  *redis.Client
	logger *slog.Logger
}

func NewLoginLimiter(redisClient *redis.Client, logger *slog.Logger) *LoginLimiter {
	if logger == nil {
		logger = slog.Default()
	}
	return &LoginLimiter{redis: redisClient, logger: logger}
}

// Allow reports whether another login attempt from sourceIP should
// proceed. Call it once per attempt, whether or not the credentials turn
// out to be valid — only counting failures would let an attacker probe
// indefinitely right up until the correct password.
func (l *LoginLimiter) Allow(ctx context.Context, sourceIP string) bool {
	key := fmt.Sprintf("fluxen:loginlimit:%s", sourceIP)
	count, err := loginRateLimitScript.Run(ctx, l.redis, []string{key}, int(loginRateLimitWindow.Seconds())).Int64()
	if err != nil {
		l.logger.Warn("auth: login rate limit check failed, failing open", "error", err)
		return true
	}
	return count <= loginRateLimitMaxAttempts
}
