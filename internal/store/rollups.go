package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"fluxen/pkg/types"
)

// ApplicationSummary is the aggregate view Application Detail's Usage &
// Cost tab and the Applications list read — "what is this application
// actually doing and spending" (Part L Phase 2 product goal), sourced
// entirely from application_daily, never from requests directly.
type ApplicationSummary struct {
	RangeStart    time.Time
	RangeEnd      time.Time
	Requests      int64
	Errors        int64
	InputTokens   int64
	OutputTokens  int64
	TotalTokens   int64
	CostMicro     types.Money
	AvgDurationMS float64
}

// DailyPoint is one day of an application's timeseries.
type DailyPoint struct {
	Day           time.Time
	Requests      int64
	Errors        int64
	InputTokens   int64
	OutputTokens  int64
	TotalTokens   int64
	CostMicro     types.Money
	AvgDurationMS float64
}

// ModelBreakdown is one (provider, model) row of an application's usage —
// the "which models/providers are responsible" half of Phase 2's exit
// criteria, sourced from request_rollup_daily.
type ModelBreakdown struct {
	Provider      string
	Model         string
	Requests      int64
	Errors        int64
	InputTokens   int64
	OutputTokens  int64
	TotalTokens   int64
	CostMicro     types.Money
	AvgDurationMS float64
}

// Rollups is the read path for the aggregates internal/rollup computes.
type Rollups struct {
	pool *pgxpool.Pool
}

func NewRollups(pool *pgxpool.Pool) *Rollups {
	return &Rollups{pool: pool}
}

// Summary aggregates application_daily for [since, now) into one row.
func (r *Rollups) Summary(ctx context.Context, appID types.AppID, since, now time.Time) (ApplicationSummary, error) {
	var requests, errs, inTok, outTok, totalTok, cost, durSum int64
	err := r.pool.QueryRow(ctx, `
		SELECT
			COALESCE(sum(requests), 0), COALESCE(sum(errors), 0),
			COALESCE(sum(input_tokens), 0), COALESCE(sum(output_tokens), 0), COALESCE(sum(total_tokens), 0),
			COALESCE(sum(cost_micro), 0), COALESCE(sum(duration_ms_sum), 0)
		FROM application_daily
		WHERE app_id = $1 AND day >= $2::date AND day < $3::date
	`, appID, since, now).Scan(&requests, &errs, &inTok, &outTok, &totalTok, &cost, &durSum)
	if err != nil {
		return ApplicationSummary{}, fmt.Errorf("store: failed to summarize application: %w", err)
	}

	return ApplicationSummary{
		RangeStart: since, RangeEnd: now,
		Requests: requests, Errors: errs,
		InputTokens: inTok, OutputTokens: outTok, TotalTokens: totalTok,
		CostMicro: types.Money(cost), AvgDurationMS: avgDuration(durSum, requests),
	}, nil
}

// Timeseries returns one DailyPoint per day of application_daily in
// [since, now), ordered chronologically. Days with no traffic simply don't
// appear — the API layer decides whether to zero-fill for charting.
func (r *Rollups) Timeseries(ctx context.Context, appID types.AppID, since, now time.Time) ([]DailyPoint, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT day, requests, errors, input_tokens, output_tokens, total_tokens, cost_micro, duration_ms_sum
		FROM application_daily
		WHERE app_id = $1 AND day >= $2::date AND day < $3::date
		ORDER BY day
	`, appID, since, now)
	if err != nil {
		return nil, fmt.Errorf("store: failed to load timeseries: %w", err)
	}
	defer rows.Close()

	var points []DailyPoint
	for rows.Next() {
		var p DailyPoint
		var cost, durSum int64
		if err := rows.Scan(&p.Day, &p.Requests, &p.Errors, &p.InputTokens, &p.OutputTokens, &p.TotalTokens, &cost, &durSum); err != nil {
			return nil, fmt.Errorf("store: failed to scan timeseries point: %w", err)
		}
		p.CostMicro = types.Money(cost)
		p.AvgDurationMS = avgDuration(durSum, p.Requests)
		points = append(points, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: failed to load timeseries: %w", err)
	}
	return points, nil
}

// ModelBreakdown returns one row per (provider, model) that served this
// application in [since, now), sorted by cost descending — the highest
// spenders lead, matching how the PRD's own examples order a model mix.
func (r *Rollups) ModelBreakdown(ctx context.Context, appID types.AppID, since, now time.Time) ([]ModelBreakdown, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT
			provider, model,
			sum(requests), COALESCE(sum(requests) FILTER (WHERE status <> 'ok'), 0),
			sum(input_tokens), sum(output_tokens), sum(total_tokens), sum(cost_micro), sum(duration_ms_sum)
		FROM request_rollup_daily
		WHERE app_id = $1 AND day >= $2::date AND day < $3::date
		GROUP BY provider, model
		ORDER BY sum(cost_micro) DESC
	`, appID, since, now)
	if err != nil {
		return nil, fmt.Errorf("store: failed to load model breakdown: %w", err)
	}
	defer rows.Close()

	var out []ModelBreakdown
	for rows.Next() {
		var m ModelBreakdown
		var cost, durSum int64
		if err := rows.Scan(&m.Provider, &m.Model, &m.Requests, &m.Errors, &m.InputTokens, &m.OutputTokens, &m.TotalTokens, &cost, &durSum); err != nil {
			return nil, fmt.Errorf("store: failed to scan model breakdown row: %w", err)
		}
		m.CostMicro = types.Money(cost)
		m.AvgDurationMS = avgDuration(durSum, m.Requests)
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: failed to load model breakdown: %w", err)
	}
	return out, nil
}

// DailyModelStat is one (day, model) point — the raw material the Token
// Efficiency detector (Part G.3.3) segments into baseline/current
// windows and diffs.
type DailyModelStat struct {
	Day             time.Time
	Model           string
	Requests        int64
	InputTokensSum  int64
	OutputTokensSum int64
}

// DailyModelStats returns one row per (day, model) in [since, until),
// collapsed over provider/status — sourced from request_rollup_daily,
// the same table Application Detail's Models tab reads, so a detector's
// numbers are always cross-checkable against what the dashboard already
// shows.
func (r *Rollups) DailyModelStats(ctx context.Context, appID types.AppID, since, until time.Time) ([]DailyModelStat, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT day, model, sum(requests), sum(input_tokens), sum(output_tokens)
		FROM request_rollup_daily
		WHERE app_id = $1 AND day >= $2::date AND day < $3::date AND status = 'ok'
		GROUP BY day, model
		ORDER BY day, model
	`, appID, since, until)
	if err != nil {
		return nil, fmt.Errorf("store: failed to load daily model stats: %w", err)
	}
	defer rows.Close()

	var out []DailyModelStat
	for rows.Next() {
		var s DailyModelStat
		if err := rows.Scan(&s.Day, &s.Model, &s.Requests, &s.InputTokensSum, &s.OutputTokensSum); err != nil {
			return nil, fmt.Errorf("store: failed to scan daily model stat: %w", err)
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: failed to load daily model stats: %w", err)
	}
	return out, nil
}

// HourlyStat is one hourly bucket, collapsed over provider/model/status —
// the raw material the Traffic Anomaly detector (Part G.3.4) computes its
// seasonal-adjusted z-scores from.
type HourlyStat struct {
	Bucket      time.Time
	Requests    int64
	Errors      int64
	TotalTokens int64
	CostMicro   int64
}

// HourlyStats returns one row per hour bucket in [since, until), sourced
// from request_rollup_hourly — the only table Part G.3.4's detector reads,
// same as every other detector's convention of never scanning `requests`
// directly for a window that can span weeks.
func (r *Rollups) HourlyStats(ctx context.Context, appID types.AppID, since, until time.Time) ([]HourlyStat, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT bucket, sum(requests), sum(requests) FILTER (WHERE status <> 'ok'), sum(total_tokens), sum(cost_micro)
		FROM request_rollup_hourly
		WHERE app_id = $1 AND bucket >= $2 AND bucket < $3
		GROUP BY bucket
		ORDER BY bucket
	`, appID, since, until)
	if err != nil {
		return nil, fmt.Errorf("store: failed to load hourly stats: %w", err)
	}
	defer rows.Close()

	var out []HourlyStat
	for rows.Next() {
		var s HourlyStat
		var errs *int64
		if err := rows.Scan(&s.Bucket, &s.Requests, &errs, &s.TotalTokens, &s.CostMicro); err != nil {
			return nil, fmt.Errorf("store: failed to scan hourly stat: %w", err)
		}
		if errs != nil {
			s.Errors = *errs
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: failed to load hourly stats: %w", err)
	}
	return out, nil
}

func avgDuration(sumMS, requests int64) float64 {
	if requests == 0 {
		return 0
	}
	return float64(sumMS) / float64(requests)
}
