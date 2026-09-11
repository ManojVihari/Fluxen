package auth

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"time"
)

// SessionTokenBytes is the amount of random entropy in a generated session
// token, before base64 encoding.
const SessionTokenBytes = 32

// Session is the shape of a dashboard login session. Phase 1 adds the
// store (Redis or Postgres-backed) and the HTTP cookie plumbing that reads
// and writes it; Phase 0 only defines the type and how a token is minted.
type Session struct {
	Token     string
	UserID    string
	ExpiresAt time.Time
}

// NewSessionToken generates a cryptographically random, URL-safe session
// token. It contains no user data — the token is an opaque lookup key into
// whatever session store Phase 1 introduces.
func NewSessionToken() (string, error) {
	buf := make([]byte, SessionTokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("auth: failed to generate session token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
