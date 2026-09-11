package auth

import "testing"

func TestGenerateAPIKey_ShapeAndVerification(t *testing.T) {
	raw, prefix, hash, err := GenerateAPIKey()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(raw) == 0 || raw[:len(KeyPrefix)] != KeyPrefix {
		t.Fatalf("expected raw key to start with %q, got %q", KeyPrefix, raw)
	}
	if len(prefix) == 0 || prefix[:len(KeyPrefix)] != KeyPrefix {
		t.Fatalf("expected prefix to start with %q, got %q", KeyPrefix, prefix)
	}
	if len(prefix) >= len(raw) {
		t.Fatalf("expected prefix to be shorter than the full raw key")
	}

	if !VerifyAPIKey(hash, raw) {
		t.Error("expected the generated raw key to verify against its own hash")
	}
	if VerifyAPIKey(hash, raw+"x") {
		t.Error("expected a tampered key to fail verification")
	}
}

func TestGenerateAPIKey_UniquePerCall(t *testing.T) {
	raw1, _, _, err := GenerateAPIKey()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	raw2, _, _, err := GenerateAPIKey()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if raw1 == raw2 {
		t.Fatal("expected two generated keys to differ")
	}
}

func TestKeyPrefixFromRaw(t *testing.T) {
	raw, wantPrefix, _, err := GenerateAPIKey()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	gotPrefix, ok := KeyPrefixFromRaw(raw)
	if !ok {
		t.Fatal("expected KeyPrefixFromRaw to succeed on a well-formed key")
	}
	if gotPrefix != wantPrefix {
		t.Errorf("expected prefix %q, got %q", wantPrefix, gotPrefix)
	}

	if _, ok := KeyPrefixFromRaw("not-a-fluxen-key"); ok {
		t.Error("expected KeyPrefixFromRaw to reject a key without the fx_live_ prefix")
	}
	if _, ok := KeyPrefixFromRaw("fx_live_"); ok {
		t.Error("expected KeyPrefixFromRaw to reject a key that's too short")
	}
}
