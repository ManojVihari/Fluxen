package auth

import "testing"

func TestHashAndVerifyPassword(t *testing.T) {
	hash, err := HashPassword("correct-horse-battery-staple")
	if err != nil {
		t.Fatalf("unexpected error hashing password: %v", err)
	}
	if hash == "" {
		t.Fatal("expected a non-empty hash")
	}
	if hash == "correct-horse-battery-staple" {
		t.Fatal("hash must not equal the plaintext password")
	}

	if !VerifyPassword(hash, "correct-horse-battery-staple") {
		t.Error("expected VerifyPassword to succeed with the correct password")
	}
	if VerifyPassword(hash, "wrong-password") {
		t.Error("expected VerifyPassword to fail with an incorrect password")
	}
}

func TestNewSessionToken_UniqueAndNonEmpty(t *testing.T) {
	a, err := NewSessionToken()
	if err != nil {
		t.Fatalf("unexpected error generating token: %v", err)
	}
	b, err := NewSessionToken()
	if err != nil {
		t.Fatalf("unexpected error generating token: %v", err)
	}

	if a == "" || b == "" {
		t.Fatal("expected non-empty tokens")
	}
	if a == b {
		t.Fatal("expected two generated tokens to differ")
	}
}
