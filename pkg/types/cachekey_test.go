package types

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestCacheKey_IdenticalRequestsMatch(t *testing.T) {
	req1 := &CanonicalRequest{Model: "gpt-4o", Messages: []Message{Message(`{"role":"user","content":"hi"}`)}}
	req2 := &CanonicalRequest{Model: "gpt-4o", Messages: []Message{Message(`{"role":"user","content":"hi"}`)}}

	k1 := CacheKey("app-1", "", req1)
	k2 := CacheKey("app-1", "", req2)

	if !bytes.Equal(k1, k2) {
		t.Error("expected identical requests to produce the same cache key")
	}
}

func TestCacheKey_DifferentMessagesDiffer(t *testing.T) {
	req1 := &CanonicalRequest{Model: "gpt-4o", Messages: []Message{Message(`{"role":"user","content":"hi"}`)}}
	req2 := &CanonicalRequest{Model: "gpt-4o", Messages: []Message{Message(`{"role":"user","content":"bye"}`)}}

	k1 := CacheKey("app-1", "", req1)
	k2 := CacheKey("app-1", "", req2)

	if bytes.Equal(k1, k2) {
		t.Error("expected different messages to produce different cache keys")
	}
}

func TestCacheKey_DifferentAppNeverCollides(t *testing.T) {
	req := &CanonicalRequest{Model: "gpt-4o", Messages: []Message{Message(`{"role":"user","content":"hi"}`)}}

	k1 := CacheKey("app-1", "", req)
	k2 := CacheKey("app-2", "", req)

	if bytes.Equal(k1, k2) {
		t.Error("expected the same request under different apps to produce different cache keys")
	}
}

func TestCacheKey_StreamAndExtraDoNotAffectTheKey(t *testing.T) {
	req1 := &CanonicalRequest{Model: "gpt-4o", Messages: []Message{Message(`{"role":"user","content":"hi"}`)}, Stream: false}
	req2 := &CanonicalRequest{
		Model: "gpt-4o", Messages: []Message{Message(`{"role":"user","content":"hi"}`)}, Stream: true,
		Extra: map[string]json.RawMessage{"user": json.RawMessage(`"alice"`)},
	}

	k1 := CacheKey("app-1", "", req1)
	k2 := CacheKey("app-1", "", req2)

	if !bytes.Equal(k1, k2) {
		t.Error("expected stream and Extra fields to not affect the cache key")
	}
}

func TestCacheKey_AppIDPrefixDoesNotCollideAcrossBoundary(t *testing.T) {
	req := &CanonicalRequest{Model: "gpt-4o"}

	// "app1"+"2ns" as a naive concatenation would equal "app12"+"ns" —
	// the null-byte separator must prevent this.
	k1 := CacheKey("app1", "2ns", req)
	k2 := CacheKey("app12", "ns", req)

	if bytes.Equal(k1, k2) {
		t.Error("expected the separator to prevent an app_id/namespace boundary collision")
	}
}
