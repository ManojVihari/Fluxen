// Package auth provides the password-hashing, session, and API-key
// primitives the dashboard and gateway use to authenticate callers:
// bcrypt password hashing and Redis-backed sessions for the dashboard
// (login/setup), and fx_-prefixed API keys with an in-process/Postgres
// resolver for the gateway (Part C.1/C.5 of the implementation
// specification).
package auth

import "golang.org/x/crypto/bcrypt"

// HashPassword hashes a plaintext password for storage. bcrypt's default
// cost is intentionally left as-is rather than tuned here — cost tuning is
// a Phase 9 security-hardening concern, not a Phase 0 one.
func HashPassword(plaintext string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(plaintext), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

// VerifyPassword reports whether plaintext matches a hash previously
// produced by HashPassword.
func VerifyPassword(hash, plaintext string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plaintext)) == nil
}
