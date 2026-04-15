package policy

import (
	"context"
	"fmt"
	"time"

	"github.com/ManojVihari/Fluxen/internal/cache"
)

// CheckRateLimit enforces per-minute rate limiting using Redis.
// rateLimit of 0 means unlimited.
func CheckRateLimit(ctx context.Context, apiKeyID int, rateLimit int) error {
	if rateLimit <= 0 {
		return nil
	}
	if cache.Client == nil {
		return nil // no Redis = skip rate limiting
	}

	key := fmt.Sprintf("ratelimit:%d", apiKeyID)
	count, err := cache.Client.Incr(ctx, key).Result()
	if err != nil {
		return nil // fail open
	}

	if count == 1 {
		cache.Client.Expire(ctx, key, 1*time.Minute)
	}

	if int(count) > rateLimit {
		return fmt.Errorf("rate limit exceeded (%d requests/min)", rateLimit)
	}

	return nil
}
