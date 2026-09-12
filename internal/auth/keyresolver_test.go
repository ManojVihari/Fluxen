package auth

import (
	"context"
	"testing"
	"time"

	"fluxen/pkg/types"
)

type fakeKeyLookup struct {
	byPrefix map[string]APIKeyRecord
	calls    int
}

func (f *fakeKeyLookup) LookupAPIKeyByPrefix(_ context.Context, prefix string) (APIKeyRecord, bool, error) {
	f.calls++
	rec, ok := f.byPrefix[prefix]
	return rec, ok, nil
}

func TestResolver_ResolvesValidKey(t *testing.T) {
	raw, prefix, hash, err := GenerateAPIKey()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	source := &fakeKeyLookup{byPrefix: map[string]APIKeyRecord{
		prefix: {ID: "key1", AppID: "app1", OrgID: "org1", KeyHash: hash},
	}}
	resolver := NewResolver(source)

	rec, err := resolver.Resolve(context.Background(), raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec.AppID != types.AppID("app1") {
		t.Errorf("expected AppID=app1, got %v", rec.AppID)
	}
}

func TestResolver_RejectsUnknownKey(t *testing.T) {
	raw, _, _, err := GenerateAPIKey()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	resolver := NewResolver(&fakeKeyLookup{byPrefix: map[string]APIKeyRecord{}})
	_, err = resolver.Resolve(context.Background(), raw)
	if err != ErrInvalidKey {
		t.Fatalf("expected ErrInvalidKey, got %v", err)
	}
}

func TestResolver_RejectsRevokedKey(t *testing.T) {
	raw, prefix, hash, err := GenerateAPIKey()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	source := &fakeKeyLookup{byPrefix: map[string]APIKeyRecord{
		prefix: {ID: "key1", AppID: "app1", OrgID: "org1", KeyHash: hash, Revoked: true},
	}}
	resolver := NewResolver(source)

	_, err = resolver.Resolve(context.Background(), raw)
	if err != ErrInvalidKey {
		t.Fatalf("expected ErrInvalidKey for a revoked key, got %v", err)
	}
}

func TestResolver_RejectsMalformedKey(t *testing.T) {
	resolver := NewResolver(&fakeKeyLookup{byPrefix: map[string]APIKeyRecord{}})
	_, err := resolver.Resolve(context.Background(), "not-a-key")
	if err != ErrInvalidKey {
		t.Fatalf("expected ErrInvalidKey for a malformed key, got %v", err)
	}
}

func TestResolver_CachesLookupsWithinTTL(t *testing.T) {
	raw, prefix, hash, err := GenerateAPIKey()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	source := &fakeKeyLookup{byPrefix: map[string]APIKeyRecord{
		prefix: {ID: "key1", AppID: "app1", OrgID: "org1", KeyHash: hash},
	}}
	resolver := NewResolver(source)

	for i := 0; i < 5; i++ {
		if _, err := resolver.Resolve(context.Background(), raw); err != nil {
			t.Fatalf("unexpected error on call %d: %v", i, err)
		}
	}

	if source.calls != 1 {
		t.Errorf("expected exactly 1 database lookup across 5 resolves within the cache TTL, got %d", source.calls)
	}
}

// TestResolver_FastPathSkipsRepeatedBcrypt guards the load-test-motivated
// optimization in Resolve: after the first bcrypt-verified resolve of a
// given raw key, later resolves of that identical key must not pay
// bcrypt's cost again. bcrypt.DefaultCost costs on the order of tens of
// milliseconds per call on typical hardware, so 500 sequential resolves
// finishing in well under a second is only possible if the fast path is
// actually skipping it — a regression back to per-call bcrypt would blow
// through this budget by 1-2 orders of magnitude, not marginally.
func TestResolver_FastPathSkipsRepeatedBcrypt(t *testing.T) {
	raw, prefix, hash, err := GenerateAPIKey()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	source := &fakeKeyLookup{byPrefix: map[string]APIKeyRecord{
		prefix: {ID: "key1", AppID: "app1", OrgID: "org1", KeyHash: hash},
	}}
	resolver := NewResolver(source)

	// One resolve to pay the unavoidable first-verification bcrypt cost
	// and populate the fast-path cache.
	if _, err := resolver.Resolve(context.Background(), raw); err != nil {
		t.Fatalf("unexpected error priming the cache: %v", err)
	}

	start := time.Now()
	for i := 0; i < 500; i++ {
		if _, err := resolver.Resolve(context.Background(), raw); err != nil {
			t.Fatalf("unexpected error on call %d: %v", i, err)
		}
	}
	elapsed := time.Since(start)
	if elapsed > time.Second {
		t.Errorf("500 resolves of the same already-verified key took %v — the bcrypt fast path appears to have regressed", elapsed)
	}
}

// TestResolver_RotatedKeySameCachedPrefixStillBcryptVerified covers the
// fast path's fallback: a different raw key presented under a prefix
// already cached from a *different* key (e.g. right after a rotation, or
// an attacker who only guessed the prefix) must still go through a real
// bcrypt check, not be waved through because a prefix cache entry
// exists.
func TestResolver_RotatedKeySameCachedPrefixStillBcryptVerified(t *testing.T) {
	raw, prefix, hash, err := GenerateAPIKey()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	source := &fakeKeyLookup{byPrefix: map[string]APIKeyRecord{
		prefix: {ID: "key1", AppID: "app1", OrgID: "org1", KeyHash: hash},
	}}
	resolver := NewResolver(source)

	if _, err := resolver.Resolve(context.Background(), raw); err != nil {
		t.Fatalf("unexpected error priming the cache: %v", err)
	}

	// Same prefix (by construction), different trailing bytes — must not
	// match the cached fingerprint, and must not verify against the
	// cached hash either.
	wrongKey := prefix + "wrong-suffix-not-the-real-key-bytes"
	if _, err := resolver.Resolve(context.Background(), wrongKey); err != ErrInvalidKey {
		t.Fatalf("expected ErrInvalidKey for a wrong key sharing a cached prefix, got %v", err)
	}
	// And the legitimate key must still resolve correctly afterward.
	if _, err := resolver.Resolve(context.Background(), raw); err != nil {
		t.Fatalf("unexpected error re-resolving the legitimate key: %v", err)
	}
}

func TestResolver_InvalidateForcesFreshLookup(t *testing.T) {
	raw, prefix, hash, err := GenerateAPIKey()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	source := &fakeKeyLookup{byPrefix: map[string]APIKeyRecord{
		prefix: {ID: "key1", AppID: "app1", OrgID: "org1", KeyHash: hash},
	}}
	resolver := NewResolver(source)

	if _, err := resolver.Resolve(context.Background(), raw); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	resolver.Invalidate(prefix)

	// Simulate revocation happening in the source of truth between the
	// first resolve and the invalidation-forced re-lookup.
	source.byPrefix[prefix] = APIKeyRecord{ID: "key1", AppID: "app1", OrgID: "org1", KeyHash: hash, Revoked: true}

	_, err = resolver.Resolve(context.Background(), raw)
	if err != ErrInvalidKey {
		t.Fatalf("expected the revocation to take effect immediately after Invalidate, got %v", err)
	}
	if source.calls != 2 {
		t.Errorf("expected a second database lookup after Invalidate, got %d calls", source.calls)
	}
}
