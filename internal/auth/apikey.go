package auth

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

// KeyPrefix is the fixed prefix on every Fluxen API key, matching the
// gateway's "fx_ key → AppID" auth contract (Part C.5).
const KeyPrefix = "fx_live_"

// prefixLookupLen is how many characters of the random token (after
// KeyPrefix) are stored, unhashed, as the indexed lookup column
// (api_keys.prefix) — enough entropy that it's not a meaningful attack
// surface on its own, short enough to index cheaply. The full raw key is
// still required and bcrypt-verified before a key resolves to an
// application; the stored prefix only narrows the lookup to one row.
const prefixLookupLen = 12

// GenerateAPIKey creates a new raw API key and its bcrypt hash. raw is
// returned to the caller exactly once — Fluxen never stores it and can
// never display it again (Part E: "api_keys ... attribution mechanism").
// prefix is the indexed lookup column; hash is what's persisted instead of
// the key itself.
func GenerateAPIKey() (raw, prefix, hash string, err error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", "", "", fmt.Errorf("auth: failed to generate api key: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(buf)

	raw = KeyPrefix + token
	prefix = KeyPrefix + token[:prefixLookupLen]

	hashed, err := bcrypt.GenerateFromPassword([]byte(raw), bcrypt.DefaultCost)
	if err != nil {
		return "", "", "", fmt.Errorf("auth: failed to hash api key: %w", err)
	}
	return raw, prefix, string(hashed), nil
}

// KeyPrefixFromRaw extracts the indexed lookup prefix from a raw key
// presented on a request, or ("", false) if it isn't shaped like a Fluxen
// key at all — callers use this to short-circuit obviously-invalid
// Authorization headers without touching the database.
func KeyPrefixFromRaw(raw string) (string, bool) {
	if !strings.HasPrefix(raw, KeyPrefix) || len(raw) < len(KeyPrefix)+prefixLookupLen {
		return "", false
	}
	return raw[:len(KeyPrefix)+prefixLookupLen], true
}

// VerifyAPIKey reports whether raw matches a hash previously produced by
// GenerateAPIKey.
func VerifyAPIKey(hash, raw string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(raw)) == nil
}
