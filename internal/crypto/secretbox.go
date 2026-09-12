// Package crypto provides the one primitive Fluxen needs for
// credentials-at-rest: symmetric AES-256-GCM encryption of small secrets
// (provider API keys, Part E.1's "provider_credentials ... encrypted").
// It is deliberately minimal — no key rotation, no key derivation from a
// password, no envelope encryption — a single 32-byte key from the
// deployment's environment (FLUXEN_ENCRYPTION_KEY), the same
// single-deployment trust model the rest of V1's config already assumes
// (Part B.1: one combined process, one Postgres, one operator).
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
)

// KeySize is the required raw key length: AES-256.
const KeySize = 32

// ErrKeyNotConfigured is returned by Seal/Open when no key was provided
// — a deployment that never sets FLUXEN_ENCRYPTION_KEY simply cannot use
// credential storage, a clear failure at the point of use rather than a
// silent no-op that would store secrets in plaintext.
var ErrKeyNotConfigured = errors.New("crypto: encryption key not configured")

// Box seals and opens secrets with one fixed AES-256-GCM key.
type Box struct {
	aead cipher.AEAD
}

// NewBox builds a Box from a raw 32-byte key. Pass DecodeKey's output.
func NewBox(key []byte) (*Box, error) {
	if len(key) != KeySize {
		return nil, fmt.Errorf("crypto: key must be %d bytes, got %d", KeySize, len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("crypto: failed to build cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("crypto: failed to build AEAD: %w", err)
	}
	return &Box{aead: aead}, nil
}

// DecodeKey parses a base64-encoded (standard, unpadded or padded) key,
// the format FLUXEN_ENCRYPTION_KEY is expected in.
func DecodeKey(encoded string) ([]byte, error) {
	key, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		// Accept raw-URL/no-padding too — operators generate this with
		// `openssl rand -base64 32`, which pads, but a hand-typed .env
		// value might not.
		key, err = base64.RawStdEncoding.DecodeString(encoded)
		if err != nil {
			return nil, fmt.Errorf("crypto: failed to decode key as base64: %w", err)
		}
	}
	if len(key) != KeySize {
		return nil, fmt.Errorf("crypto: decoded key must be %d bytes, got %d", KeySize, len(key))
	}
	return key, nil
}

// Seal encrypts plaintext, returning nonce||ciphertext||tag as one blob
// — self-contained, so Open needs nothing but the key and this blob.
func (b *Box) Seal(plaintext []byte) ([]byte, error) {
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("crypto: failed to generate nonce: %w", err)
	}
	return b.aead.Seal(nonce, nonce, plaintext, nil), nil
}

// Open reverses Seal.
func (b *Box) Open(blob []byte) ([]byte, error) {
	nonceSize := b.aead.NonceSize()
	if len(blob) < nonceSize {
		return nil, errors.New("crypto: ciphertext too short")
	}
	nonce, ciphertext := blob[:nonceSize], blob[nonceSize:]
	plaintext, err := b.aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("crypto: failed to decrypt: %w", err)
	}
	return plaintext, nil
}
