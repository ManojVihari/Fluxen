package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"fluxen/pkg/types"
)

// Measurement mirrors the measurements table (Part E.1) — the before/
// after outcome of one applied opportunity. Baseline fields are always
// populated at creation (frozen at apply time, Part G.5); observed/
// verdict fields stay nil until the interim or final check fills them
// in.
type Measurement struct {
	ID            string
	OrgID         types.OrgID
	AppID         types.AppID
	OpportunityID string
	SimulationID  *string
	PolicyVersion int
	AppliedAt     time.Time

	BaselineStart          time.Time
	BaselineEnd            time.Time
	BaselineRequests       int64
	BaselineCostMicro      int64
	BaselineCostPer1kMicro int64

	ObservedStart          *time.Time
	ObservedEnd            *time.Time
	ObservedRequests       *int64
	ObservedCostMicro      *int64
	ObservedCostPer1kMicro *int64

	ExpectedSavingsMicro int64
	ActualSavingsMicro   *int64
	ExpectedPct          float64
	ActualPct            *float64

	Verdict       *string
	VerdictReason *string

	Status      string
	FinalizedAt *time.Time
}

// Measurements is the read/write path for measurement rows.
type Measurements struct {
	pool *pgxpool.Pool
}

func NewMeasurements(pool *pgxpool.Pool) *Measurements {
	return &Measurements{pool: pool}
}

const measurementColumns = `
	id, org_id, app_id, opportunity_id, simulation_id, policy_version, applied_at,
	baseline_start, baseline_end, baseline_requests, baseline_cost_micro, baseline_cost_per_1k_micro,
	observed_start, observed_end, observed_requests, observed_cost_micro, observed_cost_per_1k_micro,
	expected_savings_micro, actual_savings_micro, expected_pct, actual_pct,
	verdict, verdict_reason,
	status, finalized_at
`

func scanMeasurement(row pgx.Row) (Measurement, error) {
	var m Measurement
	err := row.Scan(
		&m.ID, &m.OrgID, &m.AppID, &m.OpportunityID, &m.SimulationID, &m.PolicyVersion, &m.AppliedAt,
		&m.BaselineStart, &m.BaselineEnd, &m.BaselineRequests, &m.BaselineCostMicro, &m.BaselineCostPer1kMicro,
		&m.ObservedStart, &m.ObservedEnd, &m.ObservedRequests, &m.ObservedCostMicro, &m.ObservedCostPer1kMicro,
		&m.ExpectedSavingsMicro, &m.ActualSavingsMicro, &m.ExpectedPct, &m.ActualPct,
		&m.Verdict, &m.VerdictReason,
		&m.Status, &m.FinalizedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return Measurement{}, ErrNotFound
		}
		return Measurement{}, fmt.Errorf("store: failed to scan measurement: %w", err)
	}
	return m, nil
}

// Create freezes a new measurement's baseline — called once, inside
// Apply's transaction (Part G.5 step 4).
func (m *Measurements) Create(ctx context.Context, in Measurement) (Measurement, error) {
	row := m.pool.QueryRow(ctx, `
		INSERT INTO measurements (
			org_id, app_id, opportunity_id, simulation_id, policy_version, applied_at,
			baseline_start, baseline_end, baseline_requests, baseline_cost_micro, baseline_cost_per_1k_micro,
			expected_savings_micro, expected_pct, status
		) VALUES (
			$1, $2, $3, $4, $5, $6,
			$7, $8, $9, $10, $11,
			$12, $13, 'collecting'
		)
		RETURNING `+measurementColumns,
		in.OrgID, in.AppID, in.OpportunityID, in.SimulationID, in.PolicyVersion, in.AppliedAt,
		in.BaselineStart, in.BaselineEnd, in.BaselineRequests, in.BaselineCostMicro, in.BaselineCostPer1kMicro,
		in.ExpectedSavingsMicro, in.ExpectedPct,
	)
	out, err := scanMeasurement(row)
	if err != nil {
		return Measurement{}, fmt.Errorf("store: failed to create measurement: %w", err)
	}
	return out, nil
}

// Get fetches one measurement, scoped to an org.
func (m *Measurements) Get(ctx context.Context, orgID types.OrgID, id string) (Measurement, error) {
	row := m.pool.QueryRow(ctx, `
		SELECT `+measurementColumns+`
		FROM measurements
		WHERE org_id = $1 AND id = $2
	`, orgID, id)
	return scanMeasurement(row)
}

// GetByOpportunity fetches the live (non-reverted) measurement for an
// opportunity, if any — Part L Phase 6's "one live measurement per
// opportunity" invariant (also enforced by a partial unique index).
func (m *Measurements) GetByOpportunity(ctx context.Context, orgID types.OrgID, opportunityID string) (Measurement, error) {
	row := m.pool.QueryRow(ctx, `
		SELECT `+measurementColumns+`
		FROM measurements
		WHERE org_id = $1 AND opportunity_id = $2 AND status != 'reverted'
	`, orgID, opportunityID)
	return scanMeasurement(row)
}

// ListByApp returns an application's measurements, newest first.
func (m *Measurements) ListByApp(ctx context.Context, orgID types.OrgID, appID types.AppID) ([]Measurement, error) {
	rows, err := m.pool.Query(ctx, `
		SELECT `+measurementColumns+`
		FROM measurements
		WHERE org_id = $1 AND app_id = $2
		ORDER BY applied_at DESC
	`, orgID, appID)
	if err != nil {
		return nil, fmt.Errorf("store: failed to list measurements: %w", err)
	}
	defer rows.Close()

	var out []Measurement
	for rows.Next() {
		mm, err := scanMeasurement(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, mm)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: failed to list measurements: %w", err)
	}
	return out, nil
}

// Pending returns every measurement still awaiting an interim or final
// check as of now — the scheduler's own worklist, cheap thanks to the
// partial index on (collecting, interim) rows.
func (m *Measurements) Pending(ctx context.Context, now time.Time) ([]Measurement, error) {
	rows, err := m.pool.Query(ctx, `
		SELECT `+measurementColumns+`
		FROM measurements
		WHERE status IN ('collecting', 'interim') AND applied_at <= $1
		ORDER BY applied_at
	`, now)
	if err != nil {
		return nil, fmt.Errorf("store: failed to list pending measurements: %w", err)
	}
	defer rows.Close()

	var out []Measurement
	for rows.Next() {
		mm, err := scanMeasurement(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, mm)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: failed to list pending measurements: %w", err)
	}
	return out, nil
}

// RecordInterim fills in the observed/verdict fields for an interim
// check and transitions status 'collecting' -> 'interim'. Called again
// at the final check (RecordFinal) with a wider observed window.
func (m *Measurements) RecordInterim(ctx context.Context, id string, observedStart, observedEnd time.Time, observedRequests, observedCostMicro, observedCostPer1kMicro, actualSavingsMicro int64, actualPct float64, verdict, verdictReason string) (Measurement, error) {
	row := m.pool.QueryRow(ctx, `
		UPDATE measurements SET
			observed_start = $2, observed_end = $3, observed_requests = $4, observed_cost_micro = $5, observed_cost_per_1k_micro = $6,
			actual_savings_micro = $7, actual_pct = $8, verdict = $9, verdict_reason = $10,
			status = 'interim'
		WHERE id = $1
		RETURNING `+measurementColumns,
		id, observedStart, observedEnd, observedRequests, observedCostMicro, observedCostPer1kMicro,
		actualSavingsMicro, actualPct, verdict, verdictReason,
	)
	out, err := scanMeasurement(row)
	if err != nil {
		return Measurement{}, fmt.Errorf("store: failed to record interim measurement: %w", err)
	}
	return out, nil
}

// RecordFinal fills in the final observed/verdict fields and transitions
// status -> 'final', stamping finalized_at.
func (m *Measurements) RecordFinal(ctx context.Context, id string, now time.Time, observedStart, observedEnd time.Time, observedRequests, observedCostMicro, observedCostPer1kMicro, actualSavingsMicro int64, actualPct float64, verdict, verdictReason string) (Measurement, error) {
	row := m.pool.QueryRow(ctx, `
		UPDATE measurements SET
			observed_start = $2, observed_end = $3, observed_requests = $4, observed_cost_micro = $5, observed_cost_per_1k_micro = $6,
			actual_savings_micro = $7, actual_pct = $8, verdict = $9, verdict_reason = $10,
			status = 'final', finalized_at = $11
		WHERE id = $1
		RETURNING `+measurementColumns,
		id, observedStart, observedEnd, observedRequests, observedCostMicro, observedCostPer1kMicro,
		actualSavingsMicro, actualPct, verdict, verdictReason, now,
	)
	out, err := scanMeasurement(row)
	if err != nil {
		return Measurement{}, fmt.Errorf("store: failed to record final measurement: %w", err)
	}
	return out, nil
}

// MarkReverted transitions a measurement to 'reverted' — called
// alongside the opportunity's own MarkReverted (Part G.5: "the
// opportunity and its measurement both record this").
func (m *Measurements) MarkReverted(ctx context.Context, orgID types.OrgID, id string) (Measurement, error) {
	row := m.pool.QueryRow(ctx, `
		UPDATE measurements SET status = 'reverted'
		WHERE org_id = $1 AND id = $2
		RETURNING `+measurementColumns,
		orgID, id,
	)
	out, err := scanMeasurement(row)
	if err != nil {
		return Measurement{}, fmt.Errorf("store: failed to mark measurement reverted: %w", err)
	}
	return out, nil
}

// RealizedSavings sums actual_savings_micro from successful/partial
// measurements for an org — Part G.5: "realized savings is never an
// estimate," so this deliberately never touches expected_savings_micro
// or any opportunity/simulation figure. Exact-cache cache_saved_micro is
// the caller's job to add on top (a separate, non-measurement source).
func (m *Measurements) RealizedSavings(ctx context.Context, orgID types.OrgID) (int64, error) {
	var total int64
	err := m.pool.QueryRow(ctx, `
		SELECT COALESCE(sum(actual_savings_micro), 0)
		FROM measurements
		WHERE org_id = $1 AND verdict IN ('successful', 'partial')
	`, orgID).Scan(&total)
	if err != nil {
		return 0, fmt.Errorf("store: failed to sum realized savings: %w", err)
	}
	return total, nil
}
