package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"fluxen/pkg/types"
)

type Organization struct {
	ID        types.OrgID
	Name      string
	CreatedAt time.Time
}

type Organizations struct {
	pool *pgxpool.Pool
}

func NewOrganizations(pool *pgxpool.Pool) *Organizations {
	return &Organizations{pool: pool}
}

// Create inserts a new organization. Used exactly once per deployment, by
// the setup flow (Part J: "create org + owner, once").
func (o *Organizations) Create(ctx context.Context, name string) (Organization, error) {
	var org Organization
	err := o.pool.QueryRow(ctx, `
		INSERT INTO organizations (name) VALUES ($1)
		RETURNING id, name, created_at
	`, name).Scan(&org.ID, &org.Name, &org.CreatedAt)
	if err != nil {
		return Organization{}, fmt.Errorf("store: failed to create organization: %w", err)
	}
	return org, nil
}

// Any reports whether at least one organization exists — the setup
// wizard's precondition ("POST /api/v1/setup ... only callable when zero
// users exist").
func (o *Organizations) Any(ctx context.Context) (bool, error) {
	var exists bool
	err := o.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM organizations)`).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("store: failed to check for existing organizations: %w", err)
	}
	return exists, nil
}

// First returns the oldest organization, if any — used by tools/trafficgen
// so `fluxenctl seed --demo` can seed into whatever organization setup
// already created, rather than requiring a specific invocation order.
func (o *Organizations) First(ctx context.Context) (Organization, bool, error) {
	var org Organization
	err := o.pool.QueryRow(ctx, `
		SELECT id, name, created_at FROM organizations ORDER BY created_at ASC LIMIT 1
	`).Scan(&org.ID, &org.Name, &org.CreatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return Organization{}, false, nil
		}
		return Organization{}, false, fmt.Errorf("store: failed to find an organization: %w", err)
	}
	return org, true, nil
}
