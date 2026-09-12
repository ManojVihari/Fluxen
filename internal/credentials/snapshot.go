package credentials

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"

	"fluxen/pkg/providers"
	"fluxen/pkg/types"
)

const (
	snapshotTTL         = 5 * time.Second
	invalidationChannel = "fluxen:credentials:invalidate"
)

type snapshotEntry struct {
	cred      providers.Credential
	found     bool
	expiresAt time.Time
}

// Snapshot is the gateway's hot-path read for provider credentials: an
// in-process cache with a short TTL, refreshed from Postgres (via
// Service.Resolve) on expiry and invalidated immediately on a same-
// deployment change via Redis pub/sub — the exact pattern
// internal/policy.Snapshot already established for policy documents
// (Part B.2's hot-path contract: never a synchronous DB read blocking a
// proxied request; a control-plane credential change still takes effect
// within 5s even without pub/sub).
type Snapshot struct {
	service *Service
	redis   *redis.Client
	logger  *slog.Logger

	mu    sync.RWMutex
	cache map[string]snapshotEntry // keyed by provider name (org is implicit: V1 is single-org)
}

func NewSnapshot(service *Service, redisClient *redis.Client, logger *slog.Logger) *Snapshot {
	if logger == nil {
		logger = slog.Default()
	}
	return &Snapshot{service: service, redis: redisClient, cache: make(map[string]snapshotEntry), logger: logger}
}

// Get returns the org's active credential for provider, serving from
// the in-process cache when fresh. found=false means no active
// credential exists (not an error) — the caller (Server.resolveProvider)
// falls back to a static env-var credential, if any, or 503s.
func (s *Snapshot) Get(ctx context.Context, orgID types.OrgID, provider string) (cred providers.Credential, found bool, err error) {
	if entry, ok := s.fromCache(provider); ok {
		return entry.cred, entry.found, nil
	}

	cred, found, err = s.service.Resolve(ctx, orgID, provider)
	if err != nil {
		return providers.Credential{}, false, err
	}
	s.remember(provider, cred, found)
	return cred, found, nil
}

func (s *Snapshot) fromCache(provider string) (snapshotEntry, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.cache[provider]
	if !ok || time.Now().After(e.expiresAt) {
		return snapshotEntry{}, false
	}
	return e, true
}

func (s *Snapshot) remember(provider string, cred providers.Credential, found bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cache[provider] = snapshotEntry{cred: cred, found: found, expiresAt: time.Now().Add(snapshotTTL)}
}

// Invalidate drops provider's cached entry immediately and publishes to
// Redis so any other process sharing this deployment picks it up too —
// called right after Create/Revoke.
func (s *Snapshot) Invalidate(ctx context.Context, provider string) {
	s.mu.Lock()
	delete(s.cache, provider)
	s.mu.Unlock()

	if s.redis == nil {
		return
	}
	if err := s.redis.Publish(ctx, invalidationChannel, provider).Err(); err != nil {
		s.logger.Warn("credentials: failed to publish cache invalidation", "provider", provider, "error", err)
	}
}

// Subscribe listens for invalidation messages published by other
// processes. Run once at boot in its own goroutine; returns when ctx is
// canceled. A Redis outage here just falls back to the 5s TTL.
func (s *Snapshot) Subscribe(ctx context.Context) {
	if s.redis == nil {
		return
	}
	pubsub := s.redis.Subscribe(ctx, invalidationChannel)
	defer pubsub.Close()

	ch := pubsub.Channel()
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			s.mu.Lock()
			delete(s.cache, msg.Payload)
			s.mu.Unlock()
		}
	}
}
