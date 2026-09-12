package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" database/sql driver
)

// OpenPool opens a pgx connection pool for application queries. This is
// what internal/api and internal/gateway use for every real query — the
// gateway's own hot-path purity invariant means this pool is never
// touched synchronously on the request path itself, but the control API
// and the background jobs (rollup, detectors, retention) hit it
// continuously, so it's tuned for a long-running production process
// rather than left on pgxpool's bare defaults:
//   - MaxConnLifetime rotates connections periodically so a laggy
//     Postgres failover or a connection pinned to a stale query plan
//     doesn't live forever.
//   - MaxConnIdleTime releases connections back to the OS during quiet
//     periods instead of holding a pool's worth open against an idle
//     database.
func OpenPool(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	poolCfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("store: failed to parse postgres connection string: %w", err)
	}
	poolCfg.MaxConnLifetime = time.Hour
	poolCfg.MaxConnIdleTime = 30 * time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
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
