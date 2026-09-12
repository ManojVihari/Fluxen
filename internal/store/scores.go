package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"fluxen/pkg/types"
)

// Score mirrors one row of efficiency_scores — the daily Efficiency
// Score snapshot (PRD §20, Implementation Plan Phase 7).
type Score struct {
	AppID types.AppID
	Day   time.Time

	Status  string // "ok" | "insufficient_data"
	Overall int

	ModelEfficiency  int
	TokenEfficiency  int
	CacheEfficiency  int
	TrafficStability int
	CostEfficiency   int

	ComputedAt time.Time
}

// Scores is the read/write path for efficiency score snapshots.
type Scores struct {
	pool *pgxpool.Pool
}

func NewScores(pool *pgxpool.Pool) *Scores {
	return &Scores{pool: pool}
}

const scoreColumns = `
	app_id, day, status, overall,
	model_efficiency, token_efficiency, cache_efficiency, traffic_stability, cost_efficiency,
	computed_at
`

func scanScore(row pgx.Row) (Score, error) {
	var s Score
	err := row.Scan(
		&s.AppID, &s.Day, &s.Status, &s.Overall,
		&s.ModelEfficiency, &s.TokenEfficiency, &s.CacheEfficiency, &s.TrafficStability, &s.CostEfficiency,
		&s.ComputedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return Score{}, ErrNotFound
		}
		return Score{}, fmt.Errorf("store: failed to scan score: %w", err)
	}
	return s, nil
}

// UpsertDaily writes today's snapshot, or overwrites a same-day snapshot
// from an earlier run — internal/score's Runner recomputes idempotently
// (Part C.7: "compute efficiency score snapshot, daily"), same convention
// as Phase 2's rollups and Phase 3's opportunity upsert.
func (s *Scores) UpsertDaily(ctx context.Context, in Score) (Score, error) {
	row := s.pool.QueryRow(ctx, `
		INSERT INTO efficiency_scores (
			app_id, day, status, overall,
			model_efficiency, token_efficiency, cache_efficiency, traffic_stability, cost_efficiency,
			computed_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, now())
		ON CONFLICT (app_id, day) DO UPDATE SET
			status = EXCLUDED.status, overall = EXCLUDED.overall,
			model_efficiency = EXCLUDED.model_efficiency, token_efficiency = EXCLUDED.token_efficiency,
			cache_efficiency = EXCLUDED.cache_efficiency, traffic_stability = EXCLUDED.traffic_stability,
			cost_efficiency = EXCLUDED.cost_efficiency, computed_at = now()
		RETURNING `+scoreColumns,
		in.AppID, in.Day, in.Status, in.Overall,
		in.ModelEfficiency, in.TokenEfficiency, in.CacheEfficiency, in.TrafficStability, in.CostEfficiency,
	)
	out, err := scanScore(row)
	if err != nil {
		return Score{}, fmt.Errorf("store: failed to upsert score: %w", err)
	}
	return out, nil
}

// Latest returns an application's most recent snapshot.
func (s *Scores) Latest(ctx context.Context, appID types.AppID) (Score, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT `+scoreColumns+`
		FROM efficiency_scores
		WHERE app_id = $1
		ORDER BY day DESC
		LIMIT 1
	`, appID)
	return scanScore(row)
}

// History returns an application's snapshots in [since, until), oldest
// first — the input to the Efficiency tab's trend chart.
func (s *Scores) History(ctx context.Context, appID types.AppID, since, until time.Time) ([]Score, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+scoreColumns+`
		FROM efficiency_scores
		WHERE app_id = $1 AND day >= $2::date AND day < $3::date
		ORDER BY day
	`, appID, since, until)
	if err != nil {
		return nil, fmt.Errorf("store: failed to load score history: %w", err)
	}
	defer rows.Close()

	var out []Score
	for rows.Next() {
		sc, err := scanScore(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sc)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: failed to load score history: %w", err)
	}
	return out, nil
}
