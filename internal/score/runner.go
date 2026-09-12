package score

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"fluxen/internal/detect"
	"fluxen/internal/store"
	"fluxen/pkg/pricing"
)

// anomalyLookbackDays mirrors internal/detect's own traffic-anomaly
// lookback (Part G.3.4) — the score needs the same hourly history the
// detector itself would read to stay consistent with it.
const anomalyLookbackDays = 28

// tokenEffWindowDays mirrors internal/detect's own token-efficiency
// combined baseline+current window (28 + 7 days, Part G.3.3).
const tokenEffWindowDays = 35

// Runner is the only piece of internal/score that touches Postgres: it
// walks every application, fetches the same facts the four detectors
// read (plus a cost-per-request trend PeriodStats already supports), and
// upserts one daily snapshot per application via Compute.
type Runner struct {
	Apps     *store.Applications
	Rollups  *store.Rollups
	Requests *store.Requests
	Scores   *store.Scores
	Catalog  *pricing.Catalog
	Logger   *slog.Logger

	// Now is injectable for deterministic tests; nil means time.Now.
	Now func() time.Time
}

func NewRunner(apps *store.Applications, rollups *store.Rollups, requests *store.Requests, scores *store.Scores, catalog *pricing.Catalog, logger *slog.Logger) *Runner {
	if logger == nil {
		logger = slog.Default()
	}
	return &Runner{Apps: apps, Rollups: rollups, Requests: requests, Scores: scores, Catalog: catalog, Logger: logger}
}

func (r *Runner) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

// Run computes and persists today's snapshot for every application in
// the deployment. One application's failure is logged and skipped, never
// fatal to the rest (Part C.7's job contract).
func (r *Runner) Run(ctx context.Context) error {
	apps, err := r.Apps.ListAll(ctx)
	if err != nil {
		return fmt.Errorf("score: failed to list applications: %w", err)
	}

	for _, app := range apps {
		if err := r.runForApplication(ctx, app); err != nil {
			r.Logger.Error("score: failed to compute score for application", "app_id", app.ID, "error", err)
		}
	}
	return nil
}

func (r *Runner) runForApplication(ctx context.Context, app store.Application) error {
	now := r.now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	windowStart := now.AddDate(0, 0, -windowDays)

	// Rollups.Summary reads by whole calendar day; use start-of-tomorrow so
	// today's own rollup row is always included (same convention as
	// internal/detect.Runner).
	summary, err := r.Rollups.Summary(ctx, app.ID, windowStart, today.AddDate(0, 0, 1))
	if err != nil {
		return fmt.Errorf("failed to summarize application traffic: %w", err)
	}

	in := Input{
		AppID: app.ID, Now: now, Catalog: r.Catalog,
		Requests14D: summary.Requests, Spend14DMicro: int64(summary.CostMicro), TotalCostMicro: int64(summary.CostMicro),
	}

	if detect.AppMeetsFloors(summary.Requests, int64(summary.CostMicro)) {
		modelStoreFacts, err := r.Requests.ModelCostFacts(ctx, app.ID, windowStart, now)
		if err != nil {
			return fmt.Errorf("failed to load model-cost facts: %w", err)
		}
		in.ModelCostFacts = make([]detect.RequestFact, len(modelStoreFacts))
		for i, f := range modelStoreFacts {
			in.ModelCostFacts[i] = detect.RequestFact{
				RequestedModel: f.RequestedModel, Provider: f.Provider,
				InputTokens: f.InputTokens, OutputTokens: f.OutputTokens,
				HasTools: f.HasTools, HasToolCalls: f.HasToolCalls, HasImages: f.HasImages, JSONMode: f.JSONMode,
				CostMicro: f.CostMicro, CostStatus: f.CostStatus,
			}
		}

		repeatedWindowStart := now.AddDate(0, 0, -7)
		repeatedStoreFacts, err := r.Requests.RepeatedRequestFacts(ctx, app.ID, repeatedWindowStart, now)
		if err != nil {
			return fmt.Errorf("failed to load repeated-request facts: %w", err)
		}
		in.RepeatedRequestFacts = make([]detect.RepeatedRequestFact, len(repeatedStoreFacts))
		for i, f := range repeatedStoreFacts {
			in.RepeatedRequestFacts[i] = detect.RepeatedRequestFact{
				StartedAt: f.StartedAt, CacheKey: f.CacheKey, CostMicro: f.CostMicro, CostStatus: f.CostStatus,
				RequestedModel: f.RequestedModel, InputTokens: f.InputTokens, SystemPromptHash: f.SystemPromptHash,
			}
		}

		tokenEffStart := today.AddDate(0, 0, -tokenEffWindowDays)
		dailyStoreStats, err := r.Rollups.DailyModelStats(ctx, app.ID, tokenEffStart, today.AddDate(0, 0, 1))
		if err != nil {
			return fmt.Errorf("failed to load daily model stats: %w", err)
		}
		in.DailyModelStats = make([]detect.DailyModelStat, len(dailyStoreStats))
		for i, s := range dailyStoreStats {
			in.DailyModelStats[i] = detect.DailyModelStat{Day: s.Day, Model: s.Model, Requests: s.Requests, InputTokensSum: s.InputTokensSum, OutputTokensSum: s.OutputTokensSum}
		}

		hourEnd := now.Truncate(time.Hour).Add(time.Hour)
		hourlyStart := hourEnd.Add(-anomalyLookbackDays * 24 * time.Hour)
		hourlyStoreStats, err := r.Rollups.HourlyStats(ctx, app.ID, hourlyStart, hourEnd)
		if err != nil {
			return fmt.Errorf("failed to load hourly stats: %w", err)
		}
		in.HourlyStats = make([]detect.HourlyStat, len(hourlyStoreStats))
		for i, s := range hourlyStoreStats {
			in.HourlyStats[i] = detect.HourlyStat{Bucket: s.Bucket, Requests: s.Requests, Errors: s.Errors, TotalTokens: s.TotalTokens, CostMicro: s.CostMicro}
		}

		priorStart := windowStart.AddDate(0, 0, -windowDays)
		currentRequests, currentCost, err := r.Requests.PeriodStats(ctx, app.ID, windowStart, now)
		if err != nil {
			return fmt.Errorf("failed to load current-period stats: %w", err)
		}
		priorRequests, priorCost, err := r.Requests.PeriodStats(ctx, app.ID, priorStart, windowStart)
		if err != nil {
			return fmt.Errorf("failed to load prior-period stats: %w", err)
		}
		in.CurrentRequests, in.CurrentCostMicro = currentRequests, currentCost
		in.PriorRequests, in.PriorCostMicro = priorRequests, priorCost
	}

	result := Compute(in)

	_, err = r.Scores.UpsertDaily(ctx, store.Score{
		AppID: app.ID, Day: today,
		Status: result.Status, Overall: result.Overall,
		ModelEfficiency: result.Components.ModelEfficiency, TokenEfficiency: result.Components.TokenEfficiency,
		CacheEfficiency: result.Components.CacheEfficiency, TrafficStability: result.Components.TrafficStability,
		CostEfficiency: result.Components.CostEfficiency,
	})
	if err != nil {
		return fmt.Errorf("failed to persist score snapshot: %w", err)
	}
	return nil
}
