package detect

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"fluxen/internal/store"
	"fluxen/pkg/pricing"
)

// windowDays is the Model Cost detector's trailing detection window
// (Part G.3.1: "Window: trailing 14 days").
const windowDays = 14

// Runner is the only piece of internal/detect that touches Postgres: it
// walks every application, fetches its facts, runs each registered
// detector's pure logic against them, applies Part G.2's suppression
// rules, and persists what survives. The detectors themselves
// (DetectModelCost et al.) stay pure and independently unit-testable.
type Runner struct {
	Apps          *store.Applications
	Rollups       *store.Rollups
	Requests      *store.Requests
	Opportunities *store.Opportunities
	Catalog       *pricing.Catalog
	Logger        *slog.Logger

	// Now is injectable for deterministic tests; nil means time.Now.
	Now func() time.Time
}

func NewRunner(apps *store.Applications, rollups *store.Rollups, requests *store.Requests, opportunities *store.Opportunities, catalog *pricing.Catalog, logger *slog.Logger) *Runner {
	if logger == nil {
		logger = slog.Default()
	}
	return &Runner{Apps: apps, Rollups: rollups, Requests: requests, Opportunities: opportunities, Catalog: catalog, Logger: logger}
}

func (r *Runner) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

// Run detects opportunities for every application in the deployment. One
// application's failure is logged and skipped, never fatal to the rest —
// matching Part C.7's job contract ("one job's error never stops the
// scheduler or other jobs").
func (r *Runner) Run(ctx context.Context) error {
	apps, err := r.Apps.ListAll(ctx)
	if err != nil {
		return fmt.Errorf("detect: failed to list applications: %w", err)
	}

	for _, app := range apps {
		if err := r.runForApplication(ctx, app); err != nil {
			r.Logger.Error("detect: failed to run detectors for application", "app_id", app.ID, "error", err)
		}
	}
	return nil
}

func (r *Runner) runForApplication(ctx context.Context, app store.Application) error {
	now := r.now().UTC()
	windowEnd := now
	windowStart := windowEnd.AddDate(0, 0, -windowDays)

	// Rollups.Summary reads application_daily by whole calendar day
	// (day < until::date); passing the exact instant "now" as until would
	// silently exclude today's own rollup row — the same date-boundary
	// bug Phase 2 hit in internal/api/timerange.go. Use start-of-tomorrow
	// so today is always included in the floor check.
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	summary, err := r.Rollups.Summary(ctx, app.ID, windowStart, today.AddDate(0, 0, 1))
	if err != nil {
		return fmt.Errorf("failed to summarize application traffic: %w", err)
	}
	if !AppMeetsFloors(summary.Requests, int64(summary.CostMicro)) {
		// Part G.2: "an application below the traffic/spend floor
		// produces no opportunities" — nothing to detect, nothing to
		// suppress, just stop here.
		return nil
	}

	storeFacts, err := r.Requests.ModelCostFacts(ctx, app.ID, windowStart, windowEnd)
	if err != nil {
		return fmt.Errorf("failed to load model-cost facts: %w", err)
	}

	facts := make([]RequestFact, len(storeFacts))
	for i, f := range storeFacts {
		facts[i] = RequestFact{
			RequestedModel: f.RequestedModel, Provider: f.Provider,
			InputTokens: f.InputTokens, OutputTokens: f.OutputTokens,
			HasTools: f.HasTools, HasToolCalls: f.HasToolCalls, HasImages: f.HasImages, JSONMode: f.JSONMode,
			CostMicro: f.CostMicro, CostStatus: f.CostStatus,
		}
	}

	candidates := DetectModelCost(facts, r.Catalog, windowStart, windowEnd, app.ID)

	var survivors []Candidate
	for _, c := range candidates {
		if CandidateMeetsSavingsFloor(c) {
			survivors = append(survivors, c)
		}
	}
	survivors = RankAndCap(survivors)

	for _, c := range survivors {
		if err := r.persist(ctx, app, c); err != nil {
			return fmt.Errorf("failed to persist opportunity (fingerprint=%s): %w", c.Fingerprint, err)
		}
	}
	return nil
}

func (r *Runner) persist(ctx context.Context, app store.Application, c Candidate) error {
	evidence, err := json.Marshal(c.Evidence)
	if err != nil {
		return fmt.Errorf("failed to marshal evidence: %w", err)
	}
	recommendation, err := json.Marshal(c.Recommendation)
	if err != nil {
		return fmt.Errorf("failed to marshal recommendation: %w", err)
	}

	_, err = r.Opportunities.UpsertOpen(ctx, store.Opportunity{
		OrgID: app.OrgID, AppID: app.ID,
		Kind: c.Kind, Fingerprint: c.Fingerprint,
		Title: c.Title, Summary: c.Summary,
		WindowStart: c.WindowStart, WindowEnd: c.WindowEnd, SampleRequests: c.SampleRequests,
		CurrentCostMicro: c.CurrentCostMicro, ProjectedCostMicro: c.ProjectedCostMicro,
		SavingsMicro: c.SavingsMicro, SavingsPct: c.SavingsPct,
		Confidence: string(c.Confidence), ConfidenceScore: c.ConfidenceScore,
		Evidence: evidence, Recommendation: recommendation,
		DetectorVersion: c.DetectorVersion,
	})
	return err
}
