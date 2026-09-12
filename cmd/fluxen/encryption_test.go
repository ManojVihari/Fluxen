package main

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"fluxen/internal/crypto"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError + 1}))
}

func TestResolveEncryptionKey_EnvVarTakesPriority(t *testing.T) {
	dir := t.TempDir()
	raw := make([]byte, crypto.KeySize)
	for i := range raw {
		raw[i] = byte(i)
	}
	encoded := "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=" // 32 zero bytes, valid base64

	box, err := resolveEncryptionKey(encoded, dir, discardLogger())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if box == nil {
		t.Fatal("expected a non-nil box")
	}

	// The env var path must never touch the data directory at all.
	if _, err := os.Stat(filepath.Join(dir, encryptionKeyFileName)); !os.IsNotExist(err) {
		t.Errorf("expected no key file to be written when an env var key is supplied, stat err=%v", err)
	}
}

func TestResolveEncryptionKey_GeneratesAndPersistsOnFirstBoot(t *testing.T) {
	dir := t.TempDir()

	box1, err := resolveEncryptionKey("", dir, discardLogger())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if box1 == nil {
		t.Fatal("expected a generated box")
	}

	keyPath := filepath.Join(dir, encryptionKeyFileName)
	info, err := os.Stat(keyPath)
	if err != nil {
		t.Fatalf("expected the generated key to be persisted to %s: %v", keyPath, err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("expected the key file to be 0600, got %v", info.Mode().Perm())
	}

	// A blob sealed by the first boot's box must still open cleanly after
	// a fresh process "reboots" and reloads the same persisted key —
	// this is the whole point: previously-encrypted credentials must
	// remain decryptable across a container restart.
	sealed, err := box1.Seal([]byte("secret"))
	if err != nil {
		t.Fatalf("unexpected error sealing: %v", err)
	}

	box2, err := resolveEncryptionKey("", dir, discardLogger())
	if err != nil {
		t.Fatalf("unexpected error on second boot: %v", err)
	}
	opened, err := box2.Open(sealed)
	if err != nil {
		t.Fatalf("expected the second boot's key to decrypt data sealed by the first: %v", err)
	}
	if string(opened) != "secret" {
		t.Errorf("expected \"secret\", got %q", opened)
	}
}
