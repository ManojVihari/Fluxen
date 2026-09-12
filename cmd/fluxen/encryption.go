package main

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"fluxen/internal/crypto"
)

// encryptionKeyFileName is the persisted-key file inside the deployment's
// data directory (Part E.1's provider_credentials encryption key).
const encryptionKeyFileName = "encryption.key"

// resolveEncryptionKey is what makes `docker compose up` with zero
// configuration produce a fully working, encrypted-at-rest deployment —
// Settings > Providers must never require an operator to hand-generate
// and export FLUXEN_ENCRYPTION_KEY before it works.
//
// Precedence: an explicit FLUXEN_ENCRYPTION_KEY env var always wins (an
// operator who wants to manage the key themselves — a secrets manager, a
// value injected by their own orchestration — is never overridden).
// Otherwise, a key is loaded from (or, on first boot, generated into) a
// file on cfg.DataDir, which docker-compose.yml backs with its own named
// volume — separate from the Postgres volume the encrypted data itself
// lives in, so a key generated this way still isn't sitting in the same
// place as what it protects. The file persists across container
// recreation the same way the Postgres/Redis volumes already do; losing
// it (a fresh volume) makes previously-stored provider credentials
// undecryptable, exactly as losing any encryption key would.
func resolveEncryptionKey(envKey, dataDir string, logger *slog.Logger) (*crypto.Box, error) {
	if envKey != "" {
		key, err := crypto.DecodeKey(envKey)
		if err != nil {
			return nil, fmt.Errorf("fluxen: invalid FLUXEN_ENCRYPTION_KEY: %w", err)
		}
		logger.Info("fluxen: using FLUXEN_ENCRYPTION_KEY from the environment for credential encryption")
		return crypto.NewBox(key)
	}

	if dataDir == "" {
		return nil, fmt.Errorf("fluxen: no FLUXEN_ENCRYPTION_KEY set and no data directory configured to persist a generated one")
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, fmt.Errorf("fluxen: failed to create data directory %q: %w", dataDir, err)
	}
	keyPath := filepath.Join(dataDir, encryptionKeyFileName)

	if existing, err := os.ReadFile(keyPath); err == nil {
		key, err := crypto.DecodeKey(string(existing))
		if err != nil {
			return nil, fmt.Errorf("fluxen: failed to decode persisted encryption key at %s: %w", keyPath, err)
		}
		logger.Info("fluxen: loaded a previously-generated credential encryption key", "path", keyPath)
		return crypto.NewBox(key)
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("fluxen: failed to read %s: %w", keyPath, err)
	}

	raw := make([]byte, crypto.KeySize)
	if _, err := rand.Read(raw); err != nil {
		return nil, fmt.Errorf("fluxen: failed to generate an encryption key: %w", err)
	}
	encoded := base64.StdEncoding.EncodeToString(raw)
	// 0600: only this process's user can read the key back on the next
	// boot — the same file permission model an SSH private key gets.
	if err := os.WriteFile(keyPath, []byte(encoded), 0o600); err != nil {
		return nil, fmt.Errorf("fluxen: failed to persist a new encryption key to %s: %w", keyPath, err)
	}
	logger.Info("fluxen: generated a new credential encryption key on first boot", "path", keyPath)
	return crypto.NewBox(raw)
}
