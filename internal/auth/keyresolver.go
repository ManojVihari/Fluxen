package auth

import (
	"context"
	"errors"
	"sync"
	"time"

	"fluxen/pkg/types"
)

// ErrInvalidKey is returned by Resolver.Resolve for any key that doesn't
// resolve to an active application — malformed, unknown, revoked, or
// simply wrong. The gateway maps this to a single 401
// invalid_api_key response; it deliberately doesn't distinguish the
// reasons to a caller (Part C.5: "401 on failure").
var ErrInvalidKey = errors.New("auth: invalid or revoked api key")

// APIKeyRecord is what a key prefix resolves to.
type APIKeyRecord struct {
	ID      types.APIKeyID
	AppID   types.AppID
	OrgID   types.OrgID
	KeyHash string
	Revoked bool
}

// KeyLookup is the storage dependency Resolver needs — an interface
// rather than a direct internal/store import, so this package stays
// testable without a database and internal/store stays free to depend on
// internal/auth's types without creating an import cycle.
type KeyLookup interface {
	LookupAPIKeyByPrefix(ctx context.Context, prefix string) (APIKeyRecord, bool, error)
}

const (
	resolverCacheTTL     = 30 * time.Second
	resolverCacheMaxSize = 10_000 // safety valve: cache is cleared outright if it grows past this
)

type cacheEntry struct {
	rec       APIKeyRecord
	expiresAt time.Time
}

// Resolver resolves a raw API key to the application/org it belongs to.
// It caches by prefix in-process for a short TTL (Part C.5: "fx_ key →
// AppID ... no DB hit on cache hit") so the gateway's hot path avoids a
// database round trip on every request, while still re-verifying the
// bcrypt hash on every call — the cache only saves the database lookup,
// never the cryptographic check.
type Resolver struct {
	source KeyLookup

	mu    sync.RWMutex
	cache map[string]cacheEntry
}

func NewResolver(source KeyLookup) *Resolver {
	return &Resolver{source: source, cache: make(map[string]cacheEntry)}
}

// Resolve validates rawKey and returns the application it belongs to.
func (r *Resolver) Resolve(ctx context.Context, rawKey string) (APIKeyRecord, error) {
	prefix, ok := KeyPrefixFromRaw(rawKey)
	if !ok {
		return APIKeyRecord{}, ErrInvalidKey
	}

	rec, err := r.lookup(ctx, prefix)
	if err != nil {
		return APIKeyRecord{}, err
	}
	if rec.Revoked {
		return APIKeyRecord{}, ErrInvalidKey
	}
	if !VerifyAPIKey(rec.KeyHash, rawKey) {
		return APIKeyRecord{}, ErrInvalidKey
	}
	return rec, nil
}

func (r *Resolver) lookup(ctx context.Context, prefix string) (APIKeyRecord, error) {
	if rec, ok := r.fromCache(prefix); ok {
		return rec, nil
	}

	rec, ok, err := r.source.LookupAPIKeyByPrefix(ctx, prefix)
	if err != nil {
		return APIKeyRecord{}, err
	}
	if !ok {
		return APIKeyRecord{}, ErrInvalidKey
	}

	r.store(prefix, rec)
	return rec, nil
}

func (r *Resolver) fromCache(prefix string) (APIKeyRecord, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	entry, ok := r.cache[prefix]
	if !ok || time.Now().After(entry.expiresAt) {
		return APIKeyRecord{}, false
	}
	return entry.rec, true
}

func (r *Resolver) store(prefix string, rec APIKeyRecord) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if len(r.cache) >= resolverCacheMaxSize {
		r.cache = make(map[string]cacheEntry)
	}
	r.cache[prefix] = cacheEntry{rec: rec, expiresAt: time.Now().Add(resolverCacheTTL)}
}

// Invalidate drops a cached entry immediately — used after a key is
// revoked so the revocation takes effect without waiting out the TTL.
func (r *Resolver) Invalidate(prefix string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.cache, prefix)
}
