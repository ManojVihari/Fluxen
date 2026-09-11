// Package auth provides the password-hashing and session-token primitives
// the dashboard's setup/login flow will use starting in Phase 1
// ("Authentication & Applications"). Phase 0 wires this scaffolding and
// unit-tests it in isolation, but exposes no HTTP endpoint that calls it —
// there is no setup/login flow, no user-facing auth behavior, and no
// session store yet. See the implementation specification's Phase 0
// backend tasks and "Explicit non-goals."
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
