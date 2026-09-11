package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" database/sql driver
)

// OpenPool opens a pgx connection pool for application queries. This is
// what internal/api and internal/gateway will use starting in later
// phases; Phase 0 uses it only for the /readyz health check.
func OpenPool(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("store: failed to open postgres pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("store: postgres ping failed: %w", err)
	}
	return pool, nil
}

// OpenSQLDB opens a database/sql handle over the same driver, required by
// goose (which speaks database/sql, not pgx's native interface) to run
// migrations.
func OpenSQLDB(databaseURL string) (*sql.DB, error) {
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return nil, fmt.Errorf("store: failed to open sql.DB: %w", err)
	}
	return db, nil
}
