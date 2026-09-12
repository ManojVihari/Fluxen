package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"fluxen/pkg/types"
)

// Opportunity mirrors the opportunities table (Part E.1) — "the product's
// primary noun." Evidence and Recommendation stay as raw JSON here: the
// store layer never interprets a detector's evidence shape, it only
// persists and returns whatever the detector produced.
type Opportunity struct {
	ID          string
	OrgID       types.OrgID
	AppID       types.AppID
	Kind        string
	Fingerprint string
	Status      string
	Severity    *string

	Title   string
	Summary string

	WindowStart    time.Time
	WindowEnd      time.Time
	SampleRequests int64

	CurrentCostMicro   int64
	ProjectedCostMicro int64
	SavingsMicro       int64
	SavingsPct         float64

	Confidence      string
	ConfidenceScore float64

	Evidence       json.RawMessage
	Recommendation json.RawMessage

	DetectorVersion string
	DetectedAt      time.Time
	ReviewedAt      *time.Time
	ReviewedBy      *string
	DismissedAt     *time.Time
	DismissReason   *string
	LastSeenAt      time.Time
}

// Opportunities is the read/write path for detector output.
type Opportunities struct {
	pool *pgxpool.Pool
}

func NewOpportunities(pool *pgxpool.Pool) *Opportunities {
	return &Opportunities{pool: pool}
}

const opportunityColumns = `
	id, org_id, app_id, kind, fingerprint, status, severity, title, summary,
	window_start, window_end, sample_requests,
	current_cost_micro, projected_cost_micro, savings_micro, savings_pct,
	confidence, confidence_score, evidence, recommendation,
	detector_version, detected_at, reviewed_at, reviewed_by, dismissed_at, dismiss_reason,
	last_seen_at
`

func scanOpportunity(row pgx.Row) (Opportunity, error) {
	var o Opportunity
	err := row.Scan(
		&o.ID, &o.OrgID, &o.AppID, &o.Kind, &o.Fingerprint, &o.Status, &o.Severity, &o.Title, &o.Summary,
		&o.WindowStart, &o.WindowEnd, &o.SampleRequests,
		&o.CurrentCostMicro, &o.ProjectedCostMicro, &o.SavingsMicro, &o.SavingsPct,
		&o.Confidence, &o.ConfidenceScore, &o.Evidence, &o.Recommendation,
		&o.DetectorVersion, &o.DetectedAt, &o.ReviewedAt, &o.ReviewedBy, &o.DismissedAt, &o.DismissReason,
		&o.LastSeenAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return Opportunity{}, ErrNotFound
		}
		return Opportunity{}, fmt.Errorf("store: failed to scan opportunity: %w", err)
	}
	return o, nil
}

// UpsertOpen writes a freshly-detected candidate, or refreshes an
// already-live one (open/reviewed/simulated) with the same
// (app_id, fingerprint) instead of creating a duplicate (Part L Phase 3:
// "re-running the detector against unchanged traffic must not create a
// duplicate opportunity"). Refreshing never resets status/reviewed_at —
// a user's review state survives the next detector run.
func (o *Opportunities) UpsertOpen(ctx context.Context, in Opportunity) (Opportunity, error) {
	row := o.pool.QueryRow(ctx, `
		INSERT INTO opportunities (
			org_id, app_id, kind, fingerprint, status, severity, title, summary,
			window_start, window_end, sample_requests,
			current_cost_micro, projected_cost_micro, savings_micro, savings_pct,
			confidence, confidence_score, evidence, recommendation,
			detector_version, last_seen_at
		) VALUES (
			$1, $2, $3, $4, 'open', $5, $6, $7,
			$8, $9, $10,
			$11, $12, $13, $14,
			$15, $16, $17, $18,
			$19, now()
		)
		ON CONFLICT (app_id, fingerprint) WHERE status IN ('open', 'reviewed', 'simulated')
		DO UPDATE SET
			title = EXCLUDED.title, summary = EXCLUDED.summary,
			window_start = EXCLUDED.window_start, window_end = EXCLUDED.window_end,
			sample_requests = EXCLUDED.sample_requests,
			current_cost_micro = EXCLUDED.current_cost_micro,
			projected_cost_micro = EXCLUDED.projected_cost_micro,
			savings_micro = EXCLUDED.savings_micro, savings_pct = EXCLUDED.savings_pct,
			confidence = EXCLUDED.confidence, confidence_score = EXCLUDED.confidence_score,
			evidence = EXCLUDED.evidence, recommendation = EXCLUDED.recommendation,
			detector_version = EXCLUDED.detector_version, last_seen_at = now()
		RETURNING `+opportunityColumns,
		in.OrgID, in.AppID, in.Kind, in.Fingerprint, in.Severity, in.Title, in.Summary,
		in.WindowStart, in.WindowEnd, in.SampleRequests,
		in.CurrentCostMicro, in.ProjectedCostMicro, in.SavingsMicro, in.SavingsPct,
		in.Confidence, in.ConfidenceScore, in.Evidence, in.Recommendation,
		in.DetectorVersion,
	)
	out, err := scanOpportunity(row)
	if err != nil {
		return Opportunity{}, fmt.Errorf("store: failed to upsert opportunity: %w", err)
	}
	return out, nil
}

// ListByOrg returns an org's opportunities, newest-detected first,
// optionally filtered by status and/or application.
func (o *Opportunities) ListByOrg(ctx context.Context, orgID types.OrgID, status string, appID types.AppID) ([]Opportunity, error) {
	rows, err := o.pool.Query(ctx, `
		SELECT `+opportunityColumns+`
		FROM opportunities
		WHERE org_id = $1
		  AND ($2 = '' OR status = $2)
		  AND ($3 = '' OR app_id::text = $3)
		ORDER BY detected_at DESC
	`, orgID, status, appID)
	if err != nil {
		return nil, fmt.Errorf("store: failed to list opportunities: %w", err)
	}
	defer rows.Close()

	var out []Opportunity
	for rows.Next() {
		op, err := scanOpportunity(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, op)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: failed to list opportunities: %w", err)
	}
	return out, nil
}

// Get fetches one opportunity, scoped to an org so one org can never read
// another's opportunity by guessing an id.
func (o *Opportunities) Get(ctx context.Context, orgID types.OrgID, id string) (Opportunity, error) {
	row := o.pool.QueryRow(ctx, `
		SELECT `+opportunityColumns+`
		FROM opportunities
		WHERE org_id = $1 AND id = $2
	`, orgID, id)
	return scanOpportunity(row)
}

// MarkReviewed transitions an opportunity open -> reviewed (Part G.1: "a
// user opened the detail page (auto-transition on first view; also
// settable explicitly via POST /review)"). Calling it again once already
// reviewed (or in any later state) is a no-op, not an error — the action
// is idempotent.
func (o *Opportunities) MarkReviewed(ctx context.Context, orgID types.OrgID, id string) (Opportunity, error) {
	row := o.pool.QueryRow(ctx, `
		UPDATE opportunities
		SET status = 'reviewed', reviewed_at = now()
		WHERE org_id = $1 AND id = $2 AND status = 'open'
		RETURNING `+opportunityColumns,
		orgID, id,
	)
	out, err := scanOpportunity(row)
	if err == ErrNotFound {
		// Already reviewed (or in a later state) — return the current row
		// rather than treating a repeat call as an error.
		return o.Get(ctx, orgID, id)
	}
	if err != nil {
		return Opportunity{}, fmt.Errorf("store: failed to mark opportunity reviewed: %w", err)
	}
	return out, nil
}

// ErrNotApplicable is returned by MarkApplied when the opportunity is
// already applied, dismissed, stale, or reverted — Apply is only valid
// from open/reviewed/simulated (Part G.1's lifecycle), and this method
// is the single point that enforces it at the data layer.
var ErrNotApplicable = fmt.Errorf("store: opportunity is not in an applicable state")

// MarkApplied transitions an opportunity to 'applied' (Part G.1: "the
// recommended (or edited) policy change has been confirmed and
// written"). Called from inside the same transaction as the policy save
// it accompanies is not required — the opportunity and the policy are
// two independently-consistent records, and a failure here after a
// successful policy save is a real (if rare) partial-failure case the
// caller surfaces as an error rather than silently swallowing.
func (o *Opportunities) MarkApplied(ctx context.Context, orgID types.OrgID, id string) (Opportunity, error) {
	row := o.pool.QueryRow(ctx, `
		UPDATE opportunities
		SET status = 'applied'
		WHERE org_id = $1 AND id = $2 AND status IN ('open', 'reviewed', 'simulated')
		RETURNING `+opportunityColumns,
		orgID, id,
	)
	out, err := scanOpportunity(row)
	if err == ErrNotFound {
		if _, getErr := o.Get(ctx, orgID, id); getErr != nil {
			return Opportunity{}, getErr
		}
		return Opportunity{}, ErrNotApplicable
	}
	if err != nil {
		return Opportunity{}, fmt.Errorf("store: failed to mark opportunity applied: %w", err)
	}
	return out, nil
}
