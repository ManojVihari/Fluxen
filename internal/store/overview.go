package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"fluxen/pkg/types"
)

// OrgSummary is the Overview page's header tile (Part I.3): org-wide
// spend/requests/tokens for a range, sourced from application_daily
// joined to applications for org scoping (that table has no org_id
// column of its own).
type OrgSummary struct {
	Requests, Errors                       int64
	InputTokens, OutputTokens, TotalTokens int64
	CostMicro                              types.Money
}

// OrgDailyPoint is one day of the Overview page's cost-over-time chart.
type OrgDailyPoint struct {
	Day       time.Time
	CostMicro types.Money
	Requests  int64
}

// ProviderMixRow is one provider's share of an org's traffic/spend — the
// Overview page's provider-mix donut.
type ProviderMixRow struct {
	Provider  string
	CostMicro types.Money
	Requests  int64
}

// TopApplicationRow is one row of the Overview page's top-applications
// table: current-window spend, the prior equal-length window's spend
// (for a WoW-style delta), the application's latest efficiency score (nil
// if none has ever been computed), and its open opportunities' combined
// savings value.
type TopApplicationRow struct {
	AppID                types.AppID
	Slug                 string
	Name                 string
	CostMicro            types.Money
	PriorCostMicro       types.Money
	EfficiencyScore      *int
	OpenOpportunityValue int64
}

// Overview is the read path for the Overview page (Part I.3) — every
// query here is scoped by org_id, since (unlike every other screen)
// Overview deliberately aggregates across every application in the org
// at once.
type Overview struct {
	pool *pgxpool.Pool
}

func NewOverview(pool *pgxpool.Pool) *Overview {
	return &Overview{pool: pool}
}

// Summary aggregates application_daily for [since, until) across every
// application in the org.
func (o *Overview) Summary(ctx context.Context, orgID types.OrgID, since, until time.Time) (OrgSummary, error) {
	var s OrgSummary
	var cost int64
	err := o.pool.QueryRow(ctx, `
		SELECT
			COALESCE(sum(ad.requests), 0), COALESCE(sum(ad.errors), 0),
			COALESCE(sum(ad.input_tokens), 0), COALESCE(sum(ad.output_tokens), 0), COALESCE(sum(ad.total_tokens), 0),
			COALESCE(sum(ad.cost_micro), 0)
		FROM application_daily ad
		JOIN applications a ON a.id = ad.app_id
		WHERE a.org_id = $1 AND ad.day >= $2::date AND ad.day < $3::date
	`, orgID, since, until).Scan(&s.Requests, &s.Errors, &s.InputTokens, &s.OutputTokens, &s.TotalTokens, &cost)
	if err != nil {
		return OrgSummary{}, fmt.Errorf("store: failed to summarize org traffic: %w", err)
	}
	s.CostMicro = types.Money(cost)
	return s, nil
}

// Timeseries returns one OrgDailyPoint per day with traffic, org-wide.
func (o *Overview) Timeseries(ctx context.Context, orgID types.OrgID, since, until time.Time) ([]OrgDailyPoint, error) {
	rows, err := o.pool.Query(ctx, `
		SELECT ad.day, sum(ad.cost_micro), sum(ad.requests)
		FROM application_daily ad
		JOIN applications a ON a.id = ad.app_id
		WHERE a.org_id = $1 AND ad.day >= $2::date AND ad.day < $3::date
		GROUP BY ad.day
		ORDER BY ad.day
	`, orgID, since, until)
	if err != nil {
		return nil, fmt.Errorf("store: failed to load org timeseries: %w", err)
	}
	defer rows.Close()

	var out []OrgDailyPoint
	for rows.Next() {
		var p OrgDailyPoint
		var cost int64
		if err := rows.Scan(&p.Day, &cost, &p.Requests); err != nil {
			return nil, fmt.Errorf("store: failed to scan org timeseries point: %w", err)
		}
		p.CostMicro = types.Money(cost)
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: failed to load org timeseries: %w", err)
	}
	return out, nil
}

// ProviderMix aggregates request_rollup_daily by provider for [since,
// until), sorted by cost descending — request_rollup_daily carries
// org_id directly, unlike application_daily.
func (o *Overview) ProviderMix(ctx context.Context, orgID types.OrgID, since, until time.Time) ([]ProviderMixRow, error) {
	rows, err := o.pool.Query(ctx, `
		SELECT provider, sum(cost_micro), sum(requests)
		FROM request_rollup_daily
		WHERE org_id = $1 AND day >= $2::date AND day < $3::date
		GROUP BY provider
		ORDER BY sum(cost_micro) DESC
	`, orgID, since, until)
	if err != nil {
		return nil, fmt.Errorf("store: failed to load provider mix: %w", err)
	}
	defer rows.Close()

	var out []ProviderMixRow
	for rows.Next() {
		var p ProviderMixRow
		var cost int64
		if err := rows.Scan(&p.Provider, &cost, &p.Requests); err != nil {
			return nil, fmt.Errorf("store: failed to scan provider mix row: %w", err)
		}
		p.CostMicro = types.Money(cost)
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: failed to load provider mix: %w", err)
	}
	return out, nil
}

// TopApplications returns every application in the org, ranked by
// current-window spend descending and capped at limit — the Overview
// page's top-applications table. priorSince/priorUntil is the equal-
// length window immediately preceding [since, until), used for the
// delta column.
func (o *Overview) TopApplications(ctx context.Context, orgID types.OrgID, since, until, priorSince time.Time, limit int) ([]TopApplicationRow, error) {
	rows, err := o.pool.Query(ctx, `
		WITH current AS (
			SELECT app_id, sum(cost_micro) AS cost_micro
			FROM application_daily
			WHERE day >= $2::date AND day < $3::date
			GROUP BY app_id
		),
		prior AS (
			SELECT app_id, sum(cost_micro) AS cost_micro
			FROM application_daily
			WHERE day >= $4::date AND day < $2::date
			GROUP BY app_id
		),
		opp AS (
			SELECT app_id, COALESCE(sum(savings_micro), 0) AS value_micro
			FROM opportunities
			WHERE org_id = $1 AND status IN ('open', 'reviewed', 'simulated')
			GROUP BY app_id
		),
		score AS (
			SELECT DISTINCT ON (app_id) app_id, overall
			FROM efficiency_scores
			WHERE status = 'ok'
			ORDER BY app_id, day DESC
		)
		SELECT a.id, a.slug, a.name,
		       COALESCE(current.cost_micro, 0), COALESCE(prior.cost_micro, 0),
		       score.overall, COALESCE(opp.value_micro, 0)
		FROM applications a
		LEFT JOIN current ON current.app_id = a.id
		LEFT JOIN prior ON prior.app_id = a.id
		LEFT JOIN opp ON opp.app_id = a.id
		LEFT JOIN score ON score.app_id = a.id
		WHERE a.org_id = $1
		ORDER BY COALESCE(current.cost_micro, 0) DESC
		LIMIT $5
	`, orgID, since, until, priorSince, limit)
	if err != nil {
		return nil, fmt.Errorf("store: failed to load top applications: %w", err)
	}
	defer rows.Close()

	var out []TopApplicationRow
	for rows.Next() {
		var r TopApplicationRow
		var cost, priorCost int64
		if err := rows.Scan(&r.AppID, &r.Slug, &r.Name, &cost, &priorCost, &r.EfficiencyScore, &r.OpenOpportunityValue); err != nil {
			return nil, fmt.Errorf("store: failed to scan top application row: %w", err)
		}
		r.CostMicro, r.PriorCostMicro = types.Money(cost), types.Money(priorCost)
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: failed to load top applications: %w", err)
	}
	return out, nil
}
