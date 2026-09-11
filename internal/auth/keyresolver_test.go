package auth

import (
	"context"
	"testing"

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
