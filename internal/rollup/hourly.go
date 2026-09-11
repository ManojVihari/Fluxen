package rollup

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ComputeHourly recomputes request_rollup_hourly for [from, to) — every
// bucket in that range is deleted and re-aggregated directly from
// requests, inside one transaction, so a caller observing the table never
// sees a partially-recomputed window.
func ComputeHourly(ctx context.Context, pool *pgxpool.Pool, from, to time.Time) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("rollup: failed to begin hourly transaction: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op if Commit already succeeded

	if _, err := tx.Exec(ctx, `
		DELETE FROM request_rollup_hourly WHERE bucket >= $1 AND bucket < $2
	`, from, to); err != nil {
		return fmt.Errorf("rollup: failed to clear hourly window: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO request_rollup_hourly (
			org_id, app_id, bucket, provider, model, status,
			requests, input_tokens, output_tokens, total_tokens, cost_micro, duration_ms_sum
		)
		SELECT
			org_id, app_id,
			date_trunc('hour', started_at AT TIME ZONE 'UTC') AT TIME ZONE 'UTC' AS bucket,
			provider, model, status,
			count(*), sum(input_tokens), sum(output_tokens), sum(total_tokens), sum(cost_micro), sum(duration_ms)
		FROM requests
		WHERE started_at >= $1 AND started_at < $2
		GROUP BY org_id, app_id, bucket, provider, model, status
	`, from, to); err != nil {
		return fmt.Errorf("rollup: failed to compute hourly aggregates: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("rollup: failed to commit hourly transaction: %w", err)
	}
	return nil
}
