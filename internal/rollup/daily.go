package rollup

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ComputeDaily recomputes request_rollup_daily (from request_rollup_hourly
// — daily rolls up hourly rather than re-scanning requests, since hourly
// already did that work) and application_daily (from request_rollup_daily,
// collapsing the provider/model/status dimensions) for [from, to). Both
// tables are recomputed in one transaction using the same
// delete-then-insert idempotency pattern as ComputeHourly.
func ComputeDaily(ctx context.Context, pool *pgxpool.Pool, from, to time.Time) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("rollup: failed to begin daily transaction: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op if Commit already succeeded

	if _, err := tx.Exec(ctx, `
		DELETE FROM request_rollup_daily WHERE day >= $1::date AND day < $2::date
	`, from, to); err != nil {
		return fmt.Errorf("rollup: failed to clear daily window: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO request_rollup_daily (
			org_id, app_id, day, provider, model, status,
			requests, input_tokens, output_tokens, total_tokens, cost_micro, duration_ms_sum
		)
		SELECT
			org_id, app_id,
			(bucket AT TIME ZONE 'UTC')::date AS day,
			provider, model, status,
			sum(requests), sum(input_tokens), sum(output_tokens), sum(total_tokens), sum(cost_micro), sum(duration_ms_sum)
		FROM request_rollup_hourly
		WHERE bucket >= $1 AND bucket < $2
		GROUP BY org_id, app_id, day, provider, model, status
	`, from, to); err != nil {
		return fmt.Errorf("rollup: failed to compute daily aggregates: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		DELETE FROM application_daily WHERE day >= $1::date AND day < $2::date
	`, from, to); err != nil {
		return fmt.Errorf("rollup: failed to clear application_daily window: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO application_daily (
			app_id, day, requests, errors, input_tokens, output_tokens, total_tokens, cost_micro, duration_ms_sum
		)
		SELECT
			app_id, day,
			sum(requests),
			COALESCE(sum(requests) FILTER (WHERE status <> 'ok'), 0),
			sum(input_tokens), sum(output_tokens), sum(total_tokens), sum(cost_micro), sum(duration_ms_sum)
		FROM request_rollup_daily
		WHERE day >= $1::date AND day < $2::date
		GROUP BY app_id, day
	`, from, to); err != nil {
		return fmt.Errorf("rollup: failed to compute application_daily aggregates: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("rollup: failed to commit daily transaction: %w", err)
	}
	return nil
}
