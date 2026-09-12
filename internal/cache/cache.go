// Package cache is the live, Redis-backed enforcement of exact-match
// response caching (PRD §22: "Enable/disable caching"). The key function
// itself already exists (pkg/types.CacheKey, Part G.3.2, wired in since
// Phase 4) — this package only adds the store and the decision to
// actually look it up and serve from it, which Phase 1-4 never did.
package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"

	"fluxen/pkg/types"
)

// Entry is what's stored per cache key: enough to replay the response
// faithfully (streaming or not) and account for the hit correctly.
// Chunks holds each write() call's raw bytes in order for a streamed
// response — replaying them verbatim, in the same order, reproduces the
// original SSE stream byte-for-byte without needing to re-parse or
// re-synthesize it.
type Entry struct {
	Streamed     bool     `json:"streamed"`
	Body         []byte   `json:"body,omitempty"`
	Chunks       [][]byte `json:"chunks,omitempty"`
	Model        string   `json:"model"`
	InputTokens  int      `json:"input_tokens"`
	OutputTokens int      `json:"output_tokens"`
	TotalTokens  int      `json:"total_tokens"`
	CostMicro    int64    `json:"cost_micro"`
}

// Store is the Redis-backed cache. Every method fails open toward "treat
// as a miss" or "the response was already served, logging is enough" —
// a caching outage must degrade to always calling the provider, never to
// blocking traffic (the same fail-open spirit internal/guard applies to
// its own controls).
type Store struct {
	redis  *redis.Client
	logger *slog.Logger
}

func NewStore(redisClient *redis.Client, logger *slog.Logger) *Store {
	if logger == nil {
		logger = slog.Default()
	}
	return &Store{redis: redisClient, logger: logger}
}

func keyFor(appID types.AppID, cacheKey []byte) string {
	return fmt.Sprintf("fluxen:cache:%s:%x", appID, cacheKey)
}

// Get returns the cached entry for cacheKey, and whether one existed. An
// empty cacheKey (a request whose CanonicalRequest.CacheKey was never
// computed, or genuinely empty) can never hit. Any Redis error is
// treated as a miss.
func (s *Store) Get(ctx context.Context, appID types.AppID, cacheKey []byte) (Entry, bool) {
	if len(cacheKey) == 0 {
		return Entry{}, false
	}
	raw, err := s.redis.Get(ctx, keyFor(appID, cacheKey)).Bytes()
	if err != nil {
		if err != redis.Nil {
			s.logger.Warn("cache: get failed, treating as a miss", "app_id", appID, "error", err)
		}
		return Entry{}, false
	}
	var e Entry
	if err := json.Unmarshal(raw, &e); err != nil {
		s.logger.Warn("cache: failed to unmarshal cached entry, treating as a miss", "app_id", appID, "error", err)
		return Entry{}, false
	}
	return e, true
}

// Set stores an entry with ttl. Failure is logged, not fatal — the
// response was already served to the client either way; a store failure
// only means the next identical request misses too.
func (s *Store) Set(ctx context.Context, appID types.AppID, cacheKey []byte, e Entry, ttl time.Duration) {
	if len(cacheKey) == 0 || ttl <= 0 {
		return
	}
	raw, err := json.Marshal(e)
	if err != nil {
		s.logger.Warn("cache: failed to marshal entry", "app_id", appID, "error", err)
		return
	}
	if err := s.redis.Set(ctx, keyFor(appID, cacheKey), raw, ttl).Err(); err != nil {
		s.logger.Warn("cache: set failed", "app_id", appID, "error", err)
	}
}
