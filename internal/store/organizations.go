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

// RetentionSettings mirrors organizations' three retention knobs (Part
// E.2/I.6): how long raw requests are kept, how long captured
// request/response bodies are kept (a shorter, separate window), and
// whether body capture is even on — off by default, matching Part E.2's
// "off by default entirely."
type RetentionSettings struct {
	RequestsRetentionDays int
	BodyRetentionDays     int
	BodyCaptureEnabled    bool
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

// GetRetentionSettings returns an org's current retention knobs.
func (o *Organizations) GetRetentionSettings(ctx context.Context, orgID types.OrgID) (RetentionSettings, error) {
	var s RetentionSettings
	err := o.pool.QueryRow(ctx, `
		SELECT requests_retention_days, body_retention_days, body_capture_enabled
		FROM organizations WHERE id = $1
	`, orgID).Scan(&s.RequestsRetentionDays, &s.BodyRetentionDays, &s.BodyCaptureEnabled)
	if err != nil {
		if err == pgx.ErrNoRows {
			return RetentionSettings{}, ErrNotFound
		}
		return RetentionSettings{}, fmt.Errorf("store: failed to load retention settings: %w", err)
	}
	return s, nil
}

// UpdateRetentionSettings replaces an org's retention knobs.
func (o *Organizations) UpdateRetentionSettings(ctx context.Context, orgID types.OrgID, s RetentionSettings) error {
	tag, err := o.pool.Exec(ctx, `
		UPDATE organizations
		SET requests_retention_days = $2, body_retention_days = $3, body_capture_enabled = $4
		WHERE id = $1
	`, orgID, s.RequestsRetentionDays, s.BodyRetentionDays, s.BodyCaptureEnabled)
	if err != nil {
		return fmt.Errorf("store: failed to update retention settings: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// AllIDs returns every organization's id — retention enforcement (like
// detect.Runner and score.Runner) walks every org/app in the deployment,
// not just the current caller's.
func (o *Organizations) AllIDs(ctx context.Context) ([]types.OrgID, error) {
	rows, err := o.pool.Query(ctx, `SELECT id FROM organizations`)
	if err != nil {
		return nil, fmt.Errorf("store: failed to list organizations: %w", err)
	}
	defer rows.Close()

	var out []types.OrgID
	for rows.Next() {
		var id types.OrgID
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("store: failed to scan organization id: %w", err)
		}
		out = append(out, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: failed to list organizations: %w", err)
	}
	return out, nil
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
