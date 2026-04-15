package cache

import (
	"context"
	"strconv"
)

const (
	statsKeyHits       = "cachestats:hits"
	statsKeyMisses     = "cachestats:misses"
	statsKeySavedCost  = "cachestats:saved_cost"
	statsKeySavedTokens = "cachestats:saved_tokens"
)

type Stats struct {
	Hits         int64   `json:"hits"`
	Misses       int64   `json:"misses"`
	TotalRequests int64  `json:"total_requests"`
	HitRate      float64 `json:"hit_rate_percent"`
	SavedCost    float64 `json:"estimated_cost_saved_usd"`
	SavedTokens  int64   `json:"tokens_saved"`
}

// RecordHit increments hit counter and accumulates cost/token savings.
func RecordHit(ctx context.Context, savedCost float64, savedTokens int) {
	if Client == nil {
		return
	}
	pipe := Client.Pipeline()
	pipe.Incr(ctx, statsKeyHits)
	pipe.IncrByFloat(ctx, statsKeySavedCost, savedCost)
	pipe.IncrBy(ctx, statsKeySavedTokens, int64(savedTokens))
	pipe.Exec(ctx)
}

// RecordMiss increments the miss counter.
func RecordMiss(ctx context.Context) {
	if Client == nil {
		return
	}
	Client.Incr(ctx, statsKeyMisses)
}

// GetStats returns the current cache statistics.
func GetStats(ctx context.Context) (*Stats, error) {
	if Client == nil {
		return &Stats{}, nil
	}

	pipe := Client.Pipeline()
	hitsCmd := pipe.Get(ctx, statsKeyHits)
	missesCmd := pipe.Get(ctx, statsKeyMisses)
	savedCostCmd := pipe.Get(ctx, statsKeySavedCost)
	savedTokensCmd := pipe.Get(ctx, statsKeySavedTokens)
	pipe.Exec(ctx)

	hits, _ := strconv.ParseInt(hitsCmd.Val(), 10, 64)
	misses, _ := strconv.ParseInt(missesCmd.Val(), 10, 64)
	savedCost, _ := strconv.ParseFloat(savedCostCmd.Val(), 64)
	savedTokens, _ := strconv.ParseInt(savedTokensCmd.Val(), 10, 64)

	total := hits + misses
	var hitRate float64
	if total > 0 {
		hitRate = float64(hits) / float64(total) * 100
	}

	return &Stats{
		Hits:          hits,
		Misses:        misses,
		TotalRequests: total,
		HitRate:       hitRate,
		SavedCost:     savedCost,
		SavedTokens:   savedTokens,
	}, nil
}
