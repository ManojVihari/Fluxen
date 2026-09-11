package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"fluxen/pkg/types"
)

// Application mirrors the applications table's Phase 1 columns
// (Part E.1). Later phases add more read paths (summary, timeseries,
// models) without changing this shape.
type Application struct {
	ID          types.AppID
	OrgID       types.OrgID
	Slug        string
	Name        string
	Status      string
	CreatedAt   time.Time
	FirstSeenAt *time.Time
	LastSeenAt  *time.Time
}

// Applications is the Postgres-backed read/write path for applications.
type Applications struct {
	pool *pgxpool.Pool
}

func NewApplications(pool *pgxpool.Pool) *Applications {
	return &Applications{pool: pool}
}

// Create inserts a new application. slug is caller-provided (already
// derived from name and disambiguated if needed — see slugify.go) so this
// method stays a pure insert.
func (a *Applications) Create(ctx context.Context, orgID types.OrgID, slug, name string) (Application, error) {
	var app Application
	err := a.pool.QueryRow(ctx, `
		INSERT INTO applications (org_id, slug, name)
		VALUES ($1, $2, $3)
		RETURNING id, org_id, slug, name, status, created_at, first_seen_at, last_seen_at
	`, orgID, slug, name).Scan(&app.ID, &app.OrgID, &app.Slug, &app.Name, &app.Status, &app.CreatedAt, &app.FirstSeenAt, &app.LastSeenAt)
	if err != nil {
		return Application{}, fmt.Errorf("store: failed to create application: %w", err)
	}
	return app, nil
}

// ListByOrg returns every application belonging to an org, newest first.
func (a *Applications) ListByOrg(ctx context.Context, orgID types.OrgID) ([]Application, error) {
	rows, err := a.pool.Query(ctx, `
		SELECT id, org_id, slug, name, status, created_at, first_seen_at, last_seen_at
		FROM applications
		WHERE org_id = $1
		ORDER BY created_at DESC
	`, orgID)
	if err != nil {
		return nil, fmt.Errorf("store: failed to list applications: %w", err)
	}
	defer rows.Close()

	var apps []Application
	for rows.Next() {
		var app Application
		if err := rows.Scan(&app.ID, &app.OrgID, &app.Slug, &app.Name, &app.Status, &app.CreatedAt, &app.FirstSeenAt, &app.LastSeenAt); err != nil {
			return nil, fmt.Errorf("store: failed to scan application: %w", err)
		}
		apps = append(apps, app)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: failed to list applications: %w", err)
	}
	return apps, nil
}

// Get fetches one application by id, scoped to an org so one org can never
// read another's application by guessing an id.
func (a *Applications) Get(ctx context.Context, orgID types.OrgID, id types.AppID) (Application, error) {
	var app Application
	err := a.pool.QueryRow(ctx, `
		SELECT id, org_id, slug, name, status, created_at, first_seen_at, last_seen_at
		FROM applications
		WHERE org_id = $1 AND id = $2
	`, orgID, id).Scan(&app.ID, &app.OrgID, &app.Slug, &app.Name, &app.Status, &app.CreatedAt, &app.FirstSeenAt, &app.LastSeenAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return Application{}, ErrNotFound
		}
		return Application{}, fmt.Errorf("store: failed to get application: %w", err)
	}
	return app, nil
}

// SlugExists reports whether an org already has an application with this
// slug — used to disambiguate a newly-generated slug before insert.
func (a *Applications) SlugExists(ctx context.Context, orgID types.OrgID, slug string) (bool, error) {
	var exists bool
	err := a.pool.QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM applications WHERE org_id = $1 AND slug = $2)
	`, orgID, slug).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("store: failed to check slug: %w", err)
	}
	return exists, nil
}

// MarkSeen stamps first_seen_at (once) and last_seen_at (every time) for
// an application that just received traffic. This is called from the
// async ingest writer, never from the gateway's hot path (Part B.2).
func (a *Applications) MarkSeen(ctx context.Context, appID types.AppID, at time.Time) error {
	_, err := a.pool.Exec(ctx, `
		UPDATE applications
		SET last_seen_at = $2,
		    first_seen_at = COALESCE(first_seen_at, $2)
		WHERE id = $1
	`, appID, at)
	if err != nil {
		return fmt.Errorf("store: failed to mark application seen: %w", err)
	}
	return nil
}
