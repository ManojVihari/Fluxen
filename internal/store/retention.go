package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"fluxen/pkg/types"
)

// Retention is the write path for Part E.2's retention rules. It
// operates by DELETE, not partition drop: Phase 1's migration comment
// already notes that real per-month partitioning of `requests` is a
// later operational concern this codebase hasn't built yet (a single
// `requests_default` partition holds everything), so "drop the expired
// partition" isn't available as a cheap operation yet — a plain DELETE
// is the honest equivalent until that lands, at V1's traffic scale.
type Retention struct {
	pool *pgxpool.Pool
}

func NewRetention(pool *pgxpool.Pool) *Retention {
	return &Retention{pool: pool}
}

// dismissedStaleRetentionDays is Part E.2's one fixed (non-configurable)
// retention rule: "dismissed/stale opportunities: 180 days."
const dismissedStaleRetentionDays = 180

// EnforceRequests drops requests older than the org's configured
// retention window entirely, and — independently — clears
// request_body/response_body on rows older than the (always shorter)
// body retention window, regardless of whether capture is currently
// enabled (a body captured while it *was* enabled must still age out on
// schedule after the toggle is flipped off).
func (r *Retention) EnforceRequests(ctx context.Context, orgID types.OrgID, s RetentionSettings, now time.Time) (droppedRequests, clearedBodies int64, err error) {
	requestsCutoff := now.AddDate(0, 0, -s.RequestsRetentionDays)
	tag, err := r.pool.Exec(ctx, `DELETE FROM requests WHERE org_id = $1 AND started_at < $2`, orgID, requestsCutoff)
	if err != nil {
		return 0, 0, fmt.Errorf("store: failed to enforce request retention: %w", err)
	}
	droppedRequests = tag.RowsAffected()

	bodyCutoff := now.AddDate(0, 0, -s.BodyRetentionDays)
	tag, err = r.pool.Exec(ctx, `
		UPDATE requests SET request_body = NULL, response_body = NULL
		WHERE org_id = $1 AND started_at < $2 AND (request_body IS NOT NULL OR response_body IS NOT NULL)
	`, orgID, bodyCutoff)
	if err != nil {
		return droppedRequests, 0, fmt.Errorf("store: failed to enforce body retention: %w", err)
	}
	clearedBodies = tag.RowsAffected()

	return droppedRequests, clearedBodies, nil
}

// EnforceOpportunities drops dismissed/stale opportunities past Part
// E.2's fixed 180-day window.
func (r *Retention) EnforceOpportunities(ctx context.Context, orgID types.OrgID, now time.Time) (int64, error) {
	cutoff := now.AddDate(0, 0, -dismissedStaleRetentionDays)
	tag, err := r.pool.Exec(ctx, `
		DELETE FROM opportunities
		WHERE org_id = $1 AND status IN ('dismissed', 'stale') AND detected_at < $2
	`, orgID, cutoff)
	if err != nil {
		return 0, fmt.Errorf("store: failed to enforce opportunity retention: %w", err)
	}
	return tag.RowsAffected(), nil
}
