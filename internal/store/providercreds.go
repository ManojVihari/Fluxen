package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"fluxen/pkg/types"
)

// ProviderCredential mirrors provider_credentials (Part E.1).
// APIKeyEncrypted is the raw AES-GCM ciphertext — this store package
// never encrypts or decrypts it; that's internal/providercreds's job
// (Rule: stores are dumb data mappers, not business logic).
type ProviderCredential struct {
	ID              string
	OrgID           types.OrgID
	Provider        string
	APIKeyEncrypted []byte
	BaseURL         *string
	Status          string
	CreatedAt       time.Time
	RevokedAt       *time.Time

	LastHealthCheckAt     *time.Time
	LastHealthCheckStatus *string
	LastHealthCheckError  *string
}

// ProviderCredentials is the read/write path for provider credentials.
type ProviderCredentials struct {
	pool *pgxpool.Pool
}

func NewProviderCredentials(pool *pgxpool.Pool) *ProviderCredentials {
	return &ProviderCredentials{pool: pool}
}

const providerCredentialColumns = `
	id, org_id, provider, api_key_encrypted, base_url, status, created_at, revoked_at,
	last_health_check_at, last_health_check_status, last_health_check_error
`

func scanProviderCredential(row pgx.Row) (ProviderCredential, error) {
	var c ProviderCredential
	err := row.Scan(
		&c.ID, &c.OrgID, &c.Provider, &c.APIKeyEncrypted, &c.BaseURL, &c.Status, &c.CreatedAt, &c.RevokedAt,
		&c.LastHealthCheckAt, &c.LastHealthCheckStatus, &c.LastHealthCheckError,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return ProviderCredential{}, ErrNotFound
		}
		return ProviderCredential{}, fmt.Errorf("store: failed to scan provider credential: %w", err)
	}
	return c, nil
}

// Upsert replaces the org's active credential for a provider (Part E.1:
// "one active credential per (org, provider)" — a second Create call for
// the same provider revokes the first rather than creating a competing
// active row, so there is never an ambiguous "which one applies"
// question for the gateway to resolve).
func (p *ProviderCredentials) Upsert(ctx context.Context, orgID types.OrgID, provider string, apiKeyEncrypted []byte, baseURL *string) (ProviderCredential, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return ProviderCredential{}, fmt.Errorf("store: failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `
		UPDATE provider_credentials SET status = 'revoked', revoked_at = now()
		WHERE org_id = $1 AND provider = $2 AND status = 'active'
	`, orgID, provider); err != nil {
		return ProviderCredential{}, fmt.Errorf("store: failed to revoke prior credential: %w", err)
	}

	row := tx.QueryRow(ctx, `
		INSERT INTO provider_credentials (org_id, provider, api_key_encrypted, base_url)
		VALUES ($1, $2, $3, $4)
		RETURNING `+providerCredentialColumns,
		orgID, provider, apiKeyEncrypted, baseURL,
	)
	out, err := scanProviderCredential(row)
	if err != nil {
		return ProviderCredential{}, fmt.Errorf("store: failed to insert provider credential: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return ProviderCredential{}, fmt.Errorf("store: failed to commit: %w", err)
	}
	return out, nil
}

// ListByOrg returns every credential (active and revoked) for an org,
// newest first.
func (p *ProviderCredentials) ListByOrg(ctx context.Context, orgID types.OrgID) ([]ProviderCredential, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT `+providerCredentialColumns+`
		FROM provider_credentials
		WHERE org_id = $1
		ORDER BY created_at DESC
	`, orgID)
	if err != nil {
		return nil, fmt.Errorf("store: failed to list provider credentials: %w", err)
	}
	defer rows.Close()

	var out []ProviderCredential
	for rows.Next() {
		c, err := scanProviderCredential(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: failed to list provider credentials: %w", err)
	}
	return out, nil
}

// GetActive returns the org's active credential for a provider, if any.
func (p *ProviderCredentials) GetActive(ctx context.Context, orgID types.OrgID, provider string) (ProviderCredential, error) {
	row := p.pool.QueryRow(ctx, `
		SELECT `+providerCredentialColumns+`
		FROM provider_credentials
		WHERE org_id = $1 AND provider = $2 AND status = 'active'
	`, orgID, provider)
	return scanProviderCredential(row)
}

// Get fetches one credential by id, scoped to the org.
func (p *ProviderCredentials) Get(ctx context.Context, orgID types.OrgID, id string) (ProviderCredential, error) {
	row := p.pool.QueryRow(ctx, `
		SELECT `+providerCredentialColumns+`
		FROM provider_credentials
		WHERE org_id = $1 AND id = $2
	`, orgID, id)
	return scanProviderCredential(row)
}

// Revoke marks a credential revoked, scoped to the org so one org can
// never revoke another's credential by guessing an id.
func (p *ProviderCredentials) Revoke(ctx context.Context, orgID types.OrgID, id string) (ProviderCredential, error) {
	row := p.pool.QueryRow(ctx, `
		UPDATE provider_credentials SET status = 'revoked', revoked_at = now()
		WHERE org_id = $1 AND id = $2 AND status = 'active'
		RETURNING `+providerCredentialColumns,
		orgID, id,
	)
	out, err := scanProviderCredential(row)
	if err != nil {
		return ProviderCredential{}, fmt.Errorf("store: failed to revoke provider credential: %w", err)
	}
	return out, nil
}

// RecordHealthCheck stores the outcome of a health-check call, scoped to
// the org.
func (p *ProviderCredentials) RecordHealthCheck(ctx context.Context, orgID types.OrgID, id string, status string, checkErr *string) error {
	_, err := p.pool.Exec(ctx, `
		UPDATE provider_credentials
		SET last_health_check_at = now(), last_health_check_status = $3, last_health_check_error = $4
		WHERE org_id = $1 AND id = $2
	`, orgID, id, status, checkErr)
	if err != nil {
		return fmt.Errorf("store: failed to record health check: %w", err)
	}
	return nil
}
