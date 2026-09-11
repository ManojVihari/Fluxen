package auth

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"time"

	"fluxen/pkg/types"
)

// SessionTokenBytes is the amount of random entropy in a generated session
// token, before base64 encoding.
const SessionTokenBytes = 32

// SessionTTL is how long a dashboard session stays valid without renewal.
// There is no "sessions" table in the data model (Part E) — sessions are
// Redis-only, ephemeral state, which is exactly what a TTL-based store is
// for.
const SessionTTL = 7 * 24 * time.Hour

// SessionCookieName is the cookie the dashboard reads/writes.
const SessionCookieName = "fluxen_session"

// Session is the shape of a dashboard login session.
type Session struct {
	Token     string
	UserID    types.UserID
	OrgID     types.OrgID
	ExpiresAt time.Time
}

// NewSessionToken generates a cryptographically random, URL-safe session
// token. It contains no user data — the token is an opaque lookup key into
// the session store.
func NewSessionToken() (string, error) {
	buf := make([]byte, SessionTokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("auth: failed to generate session token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
