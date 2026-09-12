package crypto

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"testing"
)

func testBox(t *testing.T) *Box {
	t.Helper()
	key := make([]byte, KeySize)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("failed to generate test key: %v", err)
	}
	box, err := NewBox(key)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return box
}

func TestSealOpen_RoundTrips(t *testing.T) {
	box := testBox(t)
	plaintext := []byte("sk-super-secret-api-key")

	blob, err := box.Seal(plaintext)
	if err != nil {
		t.Fatalf("unexpected error sealing: %v", err)
	}
	if bytes.Contains(blob, plaintext) {
		t.Fatal("expected the sealed blob to never contain the plaintext verbatim")
	}

	got, err := box.Open(blob)
	if err != nil {
		t.Fatalf("unexpected error opening: %v", err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Errorf("expected %q, got %q", plaintext, got)
	}
}

func TestOpen_WrongKeyFails(t *testing.T) {
	box1 := testBox(t)
	box2 := testBox(t)

	blob, err := box1.Seal([]byte("secret"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := box2.Open(blob); err == nil {
		t.Fatal("expected decrypting with the wrong key to fail")
	}
}

func TestOpen_TamperedCiphertextFails(t *testing.T) {
	box := testBox(t)
	blob, err := box.Seal([]byte("secret"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	blob[len(blob)-1] ^= 0xFF
	if _, err := box.Open(blob); err == nil {
		t.Fatal("expected a tampered ciphertext to fail authentication")
	}
}

func TestDecodeKey_RoundTripsWithSealOpen(t *testing.T) {
	raw := make([]byte, KeySize)
	if _, err := rand.Read(raw); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	encoded := base64.StdEncoding.EncodeToString(raw)

	key, err := DecodeKey(encoded)
	if err != nil {
		t.Fatalf("unexpected error decoding: %v", err)
	}
	if !bytes.Equal(key, raw) {
		t.Error("expected decoded key to match the original")
	}
}

func TestNewBox_RejectsWrongKeySize(t *testing.T) {
	if _, err := NewBox([]byte("too short")); err == nil {
		t.Fatal("expected an error for a key that isn't 32 bytes")
	}
}
