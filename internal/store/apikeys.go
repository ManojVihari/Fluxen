package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"fluxen/internal/auth"
	"fluxen/pkg/types"
)

// APIKey mirrors the api_keys table (Part E.1). The raw key itself is
// never stored — only its prefix (indexed lookup) and bcrypt hash.
type APIKey struct {
	ID        types.APIKeyID
	AppID     types.AppID
	Name      string
	Prefix    string
	RevokedAt *time.Time
	CreatedAt time.Time
}

// APIKeys is the Postgres-backed read/write path for api_keys. It also
// implements auth.KeyLookup so the gateway's key resolver can query
// Postgres without internal/auth importing internal/store (Rule 8: keep
// business logic testable and independent of its storage — internal/auth
// depends only on the KeyLookup interface it defines).
type APIKeys struct {
	pool *pgxpool.Pool
}

func NewAPIKeys(pool *pgxpool.Pool) *APIKeys {
	return &APIKeys{pool: pool}
}

// Create issues a new key for an application. It returns the raw key
// exactly once — callers must show it to the user immediately and never
// persist it themselves.
func (k *APIKeys) Create(ctx context.Context, appID types.AppID, name string) (raw string, key APIKey, err error) {
	raw, prefix, hash, err := auth.GenerateAPIKey()
	if err != nil {
		return "", APIKey{}, err
	}

	err = k.pool.QueryRow(ctx, `
		INSERT INTO api_keys (app_id, name, prefix, key_hash)
		VALUES ($1, $2, $3, $4)
		RETURNING id, app_id, name, prefix, revoked_at, created_at
	`, appID, name, prefix, hash).Scan(&key.ID, &key.AppID, &key.Name, &key.Prefix, &key.RevokedAt, &key.CreatedAt)
	if err != nil {
		return "", APIKey{}, fmt.Errorf("store: failed to create api key: %w", err)
	}
	return raw, key, nil
}

// Revoke marks a key revoked. It scopes the update to orgID via a join so
// one org can never revoke another's key by guessing an id, and returns
// the key's prefix so the caller can invalidate any in-process resolver
// cache entry immediately rather than waiting out its TTL.
func (k *APIKeys) Revoke(ctx context.Context, orgID types.OrgID, keyID types.APIKeyID) (prefix string, err error) {
	err = k.pool.QueryRow(ctx, `
		UPDATE api_keys
		SET revoked_at = now()
		WHERE id = $1
		  AND revoked_at IS NULL
		  AND app_id IN (SELECT id FROM applications WHERE org_id = $2)
		RETURNING prefix
	`, keyID, orgID).Scan(&prefix)
	if err != nil {
		if err == pgx.ErrNoRows {
			return "", ErrNotFound
		}
		return "", fmt.Errorf("store: failed to revoke api key: %w", err)
	}
	return prefix, nil
}

// LookupAPIKeyByPrefix implements auth.KeyLookup.
func (k *APIKeys) LookupAPIKeyByPrefix(ctx context.Context, prefix string) (auth.APIKeyRecord, bool, error) {
	var rec auth.APIKeyRecord
	var revokedAt *time.Time

	err := k.pool.QueryRow(ctx, `
		SELECT ak.id, ak.app_id, a.org_id, ak.key_hash, ak.revoked_at
		FROM api_keys ak
		JOIN applications a ON a.id = ak.app_id
		WHERE ak.prefix = $1
	`, prefix).Scan(&rec.ID, &rec.AppID, &rec.OrgID, &rec.KeyHash, &revokedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return auth.APIKeyRecord{}, false, nil
		}
		return auth.APIKeyRecord{}, false, fmt.Errorf("store: failed to look up api key: %w", err)
	}
	rec.Revoked = revokedAt != nil
	return rec, true, nil
}
