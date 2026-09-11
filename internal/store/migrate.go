// Package store owns Postgres schema management (this file) and, from
// Phase 1 onward, generated/hand-written query access. Phase 0 needs only
// migration running: the embedded goose migrations applied at boot by
// cmd/fluxen and on demand by fluxenctl.
package store

import (
	"database/sql"
	"fmt"

	"github.com/pressly/goose/v3"

	fluxendb "fluxen/db"
)

const migrationsDir = "migrations"

// newGooseProvider configures goose to read migrations from the embedded
// filesystem (fluxendb.Migrations) rather than the local disk, so a
// deployed binary never depends on the migrations directory existing on
// the host.
func newGooseProvider() error {
	goose.SetBaseFS(fluxendb.Migrations)
	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("store: failed to set goose dialect: %w", err)
	}
	return nil
}

// MigrateUp applies every pending migration. db must be a *sql.DB opened
// against Postgres (via the pgx stdlib driver — see OpenSQLDB).
func MigrateUp(db *sql.DB) error {
	if err := newGooseProvider(); err != nil {
		return err
	}
	if err := goose.Up(db, migrationsDir); err != nil {
		return fmt.Errorf("store: migrate up failed: %w", err)
	}
	return nil
}

// MigrateDown rolls back exactly one migration.
func MigrateDown(db *sql.DB) error {
	if err := newGooseProvider(); err != nil {
		return err
	}
	if err := goose.Down(db, migrationsDir); err != nil {
		return fmt.Errorf("store: migrate down failed: %w", err)
	}
	return nil
}

// MigrateStatus prints the current migration status to stdout (goose's own
// behavior) and returns an error only if the check itself failed.
func MigrateStatus(db *sql.DB) error {
	if err := newGooseProvider(); err != nil {
		return err
	}
	if err := goose.Status(db, migrationsDir); err != nil {
		return fmt.Errorf("store: migrate status failed: %w", err)
	}
	return nil
}
