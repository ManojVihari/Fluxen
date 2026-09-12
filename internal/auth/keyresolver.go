package auth

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
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
	// verifiedKeySHA256 is a fast fingerprint of the exact raw key that
	// was bcrypt-verified when this entry was populated — see the fast
	// path in Resolve for why caching this (not the raw key itself, and
	// never in place of bcrypt) is safe.
	verifiedKeySHA256 [32]byte
}

// Resolver resolves a raw API key to the application/org it belongs to.
// It caches by prefix in-process for a short TTL (Part C.5: "fx_ key →
// AppID ... no DB hit on cache hit") so the gateway's hot path avoids a
// database round trip on every request.
//
// A cache hit also skips bcrypt — the expensive part, deliberately slow
// by design (bcrypt.DefaultCost) — via a fast-path fingerprint check
// instead: the entry only exists because this exact raw key was already
// bcrypt-verified once (see store/Resolve below), so a subsequent request
// presenting the identical byte-for-byte key needs only a constant-time
// SHA-256 comparison, not another ~100ms bcrypt call. This isn't a
// weaker check — an attacker without the real key still can't produce a
// matching fingerprint — it just avoids re-paying bcrypt's cost for a
// key that was already cryptographically proven correct within the TTL
// window. A request presenting a different raw key under the same
// prefix (a guessed suffix, or the app's key was rotated) falls back to
// a full bcrypt check against the cached hash — no database round trip
// needed for that either, since the hash itself is cached.
//
// This exists because load testing the gateway showed bcrypt-per-request
// capping realistic API-key-authenticated throughput at roughly 150
// req/s on constrained hardware — the fast path removes that ceiling for
// the overwhelmingly common case (one application, one key, many
// requests) while keeping the cryptographic guarantee intact.
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
	fingerprint := sha256.Sum256([]byte(rawKey))

	if entry, ok := r.fromCache(prefix); ok {
		if entry.rec.Revoked {
			return APIKeyRecord{}, ErrInvalidKey
		}
		if subtle.ConstantTimeCompare(entry.verifiedKeySHA256[:], fingerprint[:]) == 1 {
			// Fast path: this exact key was bcrypt-verified within the
			// TTL window already — see the Resolver doc comment.
			return entry.rec, nil
		}
		// Different key text under the same cached prefix (rotation, or
		// a guess) — fall through to a real bcrypt check against the
		// cached hash, no database round trip needed.
		if !VerifyAPIKey(entry.rec.KeyHash, rawKey) {
			return APIKeyRecord{}, ErrInvalidKey
		}
		r.store(prefix, entry.rec, fingerprint)
		return entry.rec, nil
	}

	rec, ok, err := r.source.LookupAPIKeyByPrefix(ctx, prefix)
	if err != nil {
		return APIKeyRecord{}, err
	}
	if !ok {
		return APIKeyRecord{}, ErrInvalidKey
	}
	if rec.Revoked {
		return APIKeyRecord{}, ErrInvalidKey
	}
	if !VerifyAPIKey(rec.KeyHash, rawKey) {
		return APIKeyRecord{}, ErrInvalidKey
	}

	r.store(prefix, rec, fingerprint)
	return rec, nil
}

func (r *Resolver) fromCache(prefix string) (cacheEntry, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	entry, ok := r.cache[prefix]
	if !ok || time.Now().After(entry.expiresAt) {
		return cacheEntry{}, false
	}
	return entry, true
}

func (r *Resolver) store(prefix string, rec APIKeyRecord, verifiedKeySHA256 [32]byte) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if len(r.cache) >= resolverCacheMaxSize {
		r.cache = make(map[string]cacheEntry)
	}
	r.cache[prefix] = cacheEntry{rec: rec, expiresAt: time.Now().Add(resolverCacheTTL), verifiedKeySHA256: verifiedKeySHA256}
}

// Invalidate drops a cached entry immediately — used after a key is
// revoked so the revocation takes effect without waiting out the TTL.
func (r *Resolver) Invalidate(prefix string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.cache, prefix)
}
