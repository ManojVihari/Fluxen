package retention

import (
	"context"
	"sync"
	"time"

	"fluxen/internal/store"
	"fluxen/pkg/types"
)

// snapshotTTL mirrors internal/policy's own 5s hot-path cache TTL — the
// gateway must never do a synchronous Postgres read per request (Part
// B.2), and a few seconds of staleness on "is body capture on" is a
// fine tradeoff (the same one every other in-process settings cache in
// this codebase already makes). Unlike policy/credentials, this cache
// has no Redis pub/sub invalidation — flipping the toggle takes up to
// snapshotTTL to actually change gateway behavior, which is acceptable
// for a low-frequency settings change.
const snapshotTTL = 5 * time.Second

type snapshotEntry struct {
	settings  store.RetentionSettings
	expiresAt time.Time
}

// Snapshot is the gateway's hot-path read for an org's retention
// settings — specifically BodyCaptureEnabled, the one field the gateway
// itself needs to decide whether to capture a request/response body at
// all.
type Snapshot struct {
	orgs *store.Organizations

	mu    sync.RWMutex
	cache map[types.OrgID]snapshotEntry
}

func NewSnapshot(orgs *store.Organizations) *Snapshot {
	return &Snapshot{orgs: orgs, cache: make(map[types.OrgID]snapshotEntry)}
}

// BodyCaptureEnabled reports whether orgID currently has body capture
// turned on, serving from the in-process cache when fresh. Any error
// fetching fresh settings fails closed (capture disabled) — a control-
// plane hiccup must never turn ON capturing of request/response bodies
// nobody asked for.
func (s *Snapshot) BodyCaptureEnabled(ctx context.Context, orgID types.OrgID) bool {
	if entry, ok := s.fromCache(orgID); ok {
		return entry.BodyCaptureEnabled
	}

	settings, err := s.orgs.GetRetentionSettings(ctx, orgID)
	if err != nil {
		return false
	}
	s.remember(orgID, settings)
	return settings.BodyCaptureEnabled
}

func (s *Snapshot) fromCache(orgID types.OrgID) (store.RetentionSettings, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.cache[orgID]
	if !ok || time.Now().After(e.expiresAt) {
		return store.RetentionSettings{}, false
	}
	return e.settings, true
}

func (s *Snapshot) remember(orgID types.OrgID, settings store.RetentionSettings) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cache[orgID] = snapshotEntry{settings: settings, expiresAt: time.Now().Add(snapshotTTL)}
}
