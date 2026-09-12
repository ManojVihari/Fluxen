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

// Simulation mirrors the simulations table (Part E.1) — a scenario run
// against real historical facts, append-only.
type Simulation struct {
	ID            string
	OrgID         types.OrgID
	AppID         types.AppID
	OpportunityID *string

	Scenario json.RawMessage

	WindowStart time.Time
	WindowEnd   time.Time

	ReplayedRequests int64
	AffectedRequests int64
	Sampled          bool

	ActualCostMicro              int64
	SimulatedCostMicro           int64
	DeltaMicro                   int64
	DeltaPct                     float64
	ProjectedMonthlySavingsMicro int64

	Breakdown   json.RawMessage
	Assumptions json.RawMessage

	EngineVersion string
	CreatedBy     *string
	CreatedAt     time.Time
}

// Simulations is the read/write path for simulation runs.
type Simulations struct {
	pool *pgxpool.Pool
}

func NewSimulations(pool *pgxpool.Pool) *Simulations {
	return &Simulations{pool: pool}
}

const simulationColumns = `
	id, org_id, app_id, opportunity_id, scenario,
	window_start, window_end,
	replayed_requests, affected_requests, sampled,
	actual_cost_micro, simulated_cost_micro, delta_micro, delta_pct, projected_monthly_savings_micro,
	breakdown, assumptions,
	engine_version, created_by, created_at
`

func scanSimulation(row pgx.Row) (Simulation, error) {
	var s Simulation
	err := row.Scan(
		&s.ID, &s.OrgID, &s.AppID, &s.OpportunityID, &s.Scenario,
		&s.WindowStart, &s.WindowEnd,
		&s.ReplayedRequests, &s.AffectedRequests, &s.Sampled,
		&s.ActualCostMicro, &s.SimulatedCostMicro, &s.DeltaMicro, &s.DeltaPct, &s.ProjectedMonthlySavingsMicro,
		&s.Breakdown, &s.Assumptions,
		&s.EngineVersion, &s.CreatedBy, &s.CreatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return Simulation{}, ErrNotFound
		}
		return Simulation{}, fmt.Errorf("store: failed to scan simulation: %w", err)
	}
	return s, nil
}

// Create persists a completed simulation run (Phase 4 computes results
// synchronously — there is no pending/running state to write ahead of
// it, at V1's replay scale).
func (s *Simulations) Create(ctx context.Context, in Simulation) (Simulation, error) {
	row := s.pool.QueryRow(ctx, `
		INSERT INTO simulations (
			org_id, app_id, opportunity_id, scenario,
			window_start, window_end,
			replayed_requests, affected_requests, sampled,
			actual_cost_micro, simulated_cost_micro, delta_micro, delta_pct, projected_monthly_savings_micro,
			breakdown, assumptions,
			engine_version, created_by
		) VALUES (
			$1, $2, $3, $4,
			$5, $6,
			$7, $8, $9,
			$10, $11, $12, $13, $14,
			$15, $16,
			$17, $18
		)
		RETURNING `+simulationColumns,
		in.OrgID, in.AppID, in.OpportunityID, in.Scenario,
		in.WindowStart, in.WindowEnd,
		in.ReplayedRequests, in.AffectedRequests, in.Sampled,
		in.ActualCostMicro, in.SimulatedCostMicro, in.DeltaMicro, in.DeltaPct, in.ProjectedMonthlySavingsMicro,
		in.Breakdown, in.Assumptions,
		in.EngineVersion, in.CreatedBy,
	)
	out, err := scanSimulation(row)
	if err != nil {
		return Simulation{}, fmt.Errorf("store: failed to create simulation: %w", err)
	}
	return out, nil
}

// Get fetches one simulation, scoped to an org.
func (s *Simulations) Get(ctx context.Context, orgID types.OrgID, id string) (Simulation, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT `+simulationColumns+`
		FROM simulations
		WHERE org_id = $1 AND id = $2
	`, orgID, id)
	return scanSimulation(row)
}

// ListByApp returns an application's simulations, newest first.
func (s *Simulations) ListByApp(ctx context.Context, orgID types.OrgID, appID types.AppID) ([]Simulation, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+simulationColumns+`
		FROM simulations
		WHERE org_id = $1 AND app_id = $2
		ORDER BY created_at DESC
	`, orgID, appID)
	if err != nil {
		return nil, fmt.Errorf("store: failed to list simulations: %w", err)
	}
	defer rows.Close()

	var out []Simulation
	for rows.Next() {
		sim, err := scanSimulation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sim)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: failed to list simulations: %w", err)
	}
	return out, nil
}
