package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"fluxen/pkg/types"
)

// ErrSessionNotFound is returned when a session token doesn't resolve —
// expired, revoked (logout), or never valid.
var ErrSessionNotFound = errors.New("auth: session not found")

const sessionKeyPrefix = "fluxen:session:"

// SessionStore persists dashboard login sessions in Redis, keyed by token
// with Redis's own TTL doing expiry — there is no sessions table (Part E),
// so this is the session store, not a cache in front of one.
type SessionStore struct {
	redis *redis.Client
}

func NewSessionStore(client *redis.Client) *SessionStore {
	return &SessionStore{redis: client}
}

type storedSession struct {
	UserID string `json:"user_id"`
	OrgID  string `json:"org_id"`
}

// Create mints a new session for (userID, orgID) and persists it with
// SessionTTL, returning the raw token to set as the session cookie.
func (s *SessionStore) Create(ctx context.Context, userID, orgID string) (Session, error) {
	token, err := NewSessionToken()
	if err != nil {
		return Session{}, err
	}

	payload, err := json.Marshal(storedSession{UserID: userID, OrgID: orgID})
	if err != nil {
		return Session{}, fmt.Errorf("auth: failed to marshal session: %w", err)
	}

	if err := s.redis.Set(ctx, sessionKeyPrefix+token, payload, SessionTTL).Err(); err != nil {
		return Session{}, fmt.Errorf("auth: failed to persist session: %w", err)
	}

	return Session{
		Token:     token,
		UserID:    types.UserID(userID),
		OrgID:     types.OrgID(orgID),
		ExpiresAt: time.Now().Add(SessionTTL),
	}, nil
}

// Get resolves a session token. It returns ErrSessionNotFound for an
// expired, revoked, or unknown token — the caller (session middleware)
// maps that to 401, same as an invalid API key maps to 401 on the gateway
// side.
func (s *SessionStore) Get(ctx context.Context, token string) (Session, error) {
	payload, err := s.redis.Get(ctx, sessionKeyPrefix+token).Bytes()
	if errors.Is(err, redis.Nil) {
		return Session{}, ErrSessionNotFound
	}
	if err != nil {
		return Session{}, fmt.Errorf("auth: failed to read session: %w", err)
	}

	var stored storedSession
	if err := json.Unmarshal(payload, &stored); err != nil {
		return Session{}, fmt.Errorf("auth: failed to unmarshal session: %w", err)
	}

	return Session{
		Token:  token,
		UserID: types.UserID(stored.UserID),
		OrgID:  types.OrgID(stored.OrgID),
	}, nil
}

// Delete revokes a session immediately (logout).
func (s *SessionStore) Delete(ctx context.Context, token string) error {
	return s.redis.Del(ctx, sessionKeyPrefix+token).Err()
}
