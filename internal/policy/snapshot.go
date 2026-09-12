package policy

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"

	corepolicy "fluxen/pkg/policy"
	"fluxen/pkg/types"
)

const (
	snapshotTTL         = 5 * time.Second
	invalidationChannel = "fluxen:policy:invalidate"
)

type snapshotEntry struct {
	doc       corepolicy.PolicyDocument
	version   int
	expiresAt time.Time
}

// Snapshot is the gateway's read path for policy documents: an
// in-process cache with a short TTL (Part C.5: 5s), refreshed from
// Postgres on expiry and invalidated immediately on a same-deployment
// change via Redis pub/sub. A single-process deployment (Part B.1's
// combined cmd/fluxen binary) never actually needs the pub/sub path —
// its own Save already calls Invalidate directly — but Subscribe exists
// for the split-deployment case (cmd/fluxen-gateway + cmd/fluxen-control
// as separate processes) where a control-plane apply must still take
// effect on the gateway process within the TTL.
type Snapshot struct {
	store  *Store
	redis  *redis.Client
	logger *slog.Logger

	mu    sync.RWMutex
	cache map[types.AppID]snapshotEntry
}

func NewSnapshot(store *Store, redisClient *redis.Client, logger *slog.Logger) *Snapshot {
	if logger == nil {
		logger = slog.Default()
	}
	return &Snapshot{store: store, redis: redisClient, cache: make(map[types.AppID]snapshotEntry), logger: logger}
}

// Get returns the current policy document and version for appID, serving
// from the in-process cache when fresh.
func (s *Snapshot) Get(ctx context.Context, appID types.AppID) (corepolicy.PolicyDocument, int, error) {
	if entry, ok := s.fromCache(appID); ok {
		return entry.doc, entry.version, nil
	}

	rec, err := s.store.Get(ctx, appID)
	if err != nil {
		return corepolicy.PolicyDocument{}, 0, err
	}
	s.remember(appID, rec.Document, rec.Version)
	return rec.Document, rec.Version, nil
}

func (s *Snapshot) fromCache(appID types.AppID) (snapshotEntry, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.cache[appID]
	if !ok || time.Now().After(e.expiresAt) {
		return snapshotEntry{}, false
	}
	return e, true
}

func (s *Snapshot) remember(appID types.AppID, doc corepolicy.PolicyDocument, version int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cache[appID] = snapshotEntry{doc: doc, version: version, expiresAt: time.Now().Add(snapshotTTL)}
}

// Invalidate drops appID's cached entry immediately — called right after
// a local Save so this same process never serves a stale document even
// within the TTL — and publishes to Redis so any other process sharing
// this deployment picks it up too, instead of waiting out the TTL.
func (s *Snapshot) Invalidate(ctx context.Context, appID types.AppID) {
	s.mu.Lock()
	delete(s.cache, appID)
	s.mu.Unlock()

	if s.redis == nil {
		return
	}
	if err := s.redis.Publish(ctx, invalidationChannel, string(appID)).Err(); err != nil {
		s.logger.Warn("policy: failed to publish cache invalidation", "app_id", appID, "error", err)
	}
}

// Subscribe listens for invalidation messages published by other
// processes and drops the corresponding local cache entry. Run once at
// boot in its own goroutine; returns when ctx is canceled. A Redis
// outage here means this process simply falls back to its 5s TTL — never
// fatal (Rule 20's fail-open spirit applies to the cache layer too, not
// just guard's enforcement checks).
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
			delete(s.cache, types.AppID(msg.Payload))
			s.mu.Unlock()
		}
	}
}
