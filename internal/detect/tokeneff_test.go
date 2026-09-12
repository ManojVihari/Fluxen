package detect

import (
	"testing"
	"time"

	"fluxen/pkg/pricing"
	"fluxen/pkg/types"
)

func tokenEffCatalog(t *testing.T) *pricing.Catalog {
	t.Helper()
	c, err := pricing.LoadEmbedded()
	if err != nil {
		t.Fatalf("failed to load embedded pricing catalog: %v", err)
	}
	return c
}

// dailyStat builds one (day, model) point with the given request count and
// a flat per-request token count for both dimensions.
func dailyStat(day time.Time, model string, requests int64, inputPerReq, outputPerReq int64) DailyModelStat {
	return DailyModelStat{
		Day: day, Model: model, Requests: requests,
		InputTokensSum: requests * inputPerReq, OutputTokensSum: requests * outputPerReq,
	}
}

// TestDetectTokenEfficiency_TokenGrowthFixtureProducesOpportunity is the
// detector-quality "token-growth" fixture (Part K): a flat 28-day
// baseline followed by a trailing 7-day window with a clean, sustained
// jump in input tokens per request.
func TestDetectTokenEfficiency_TokenGrowthFixtureProducesOpportunity(t *testing.T) {
	catalog := tokenEffCatalog(t)
	now := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	currentEnd := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	currentStart := currentEnd.AddDate(0, 0, -tokenEffCurrentDays)
	baselineStart := currentStart.AddDate(0, 0, -tokenEffBaselineDays)

	// gpt-4-turbo's catalog price ($10/Mtok input) makes a realistic
	// per-request token drift clear the $10/5% dollar floors within a
	// modest, human-scale request volume.
	var stats []DailyModelStat
	// Baseline: 28 days, 100 requests/day, 1000 input / 200 output tokens.
	for d := baselineStart; d.Before(currentStart); d = d.AddDate(0, 0, 1) {
		stats = append(stats, dailyStat(d, "gpt-4-turbo", 100, 1000, 200))
	}
	// Current: 7 days, 100 requests/day, input tokens jump 40% to 1400.
	for d := currentStart; d.Before(currentEnd); d = d.AddDate(0, 0, 1) {
		stats = append(stats, dailyStat(d, "gpt-4-turbo", 100, 1400, 200))
	}

	candidates := DetectTokenEfficiency(stats, now, types.AppID("app-1"), catalog)
	if len(candidates) != 1 {
		t.Fatalf("expected exactly one candidate, got %d", len(candidates))
	}
	c := candidates[0]
	if c.Kind != KindTokenEfficiency {
		t.Errorf("expected kind token_efficiency, got %q", c.Kind)
	}
	ev, ok := c.Evidence.(TokenEfficiencyEvidence)
	if !ok {
		t.Fatalf("expected TokenEfficiencyEvidence, got %T", c.Evidence)
	}
	if ev.Dimension != "input" {
		t.Errorf("expected input dimension to drift, got %q", ev.Dimension)
	}
	if ev.DriftPct < inputDriftThreshold {
		t.Errorf("expected drift >= %.2f, got %.2f", inputDriftThreshold, ev.DriftPct)
	}
	if c.SavingsMicro <= 0 {
		t.Error("expected positive cost impact")
	}
	rec, ok := c.Recommendation.(TokenEfficiencyRecommendation)
	if !ok || rec.Action != "investigate_prompt_drift" {
		t.Fatalf("expected an investigate_prompt_drift recommendation, got %+v", c.Recommendation)
	}
}

// TestDetectTokenEfficiency_CleanFixtureProducesNothing is the
// release-blocking zero-false-positive check: flat, unchanged usage
// across baseline and current windows must produce no candidates.
func TestDetectTokenEfficiency_CleanFixtureProducesNothing(t *testing.T) {
	catalog := tokenEffCatalog(t)
	now := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	currentEnd := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	baselineStart := currentEnd.AddDate(0, 0, -(tokenEffCurrentDays + tokenEffBaselineDays))

	var stats []DailyModelStat
	for d := baselineStart; d.Before(currentEnd); d = d.AddDate(0, 0, 1) {
		stats = append(stats, dailyStat(d, "gpt-4o-mini", 100, 1000, 200))
	}

	candidates := DetectTokenEfficiency(stats, now, types.AppID("app-1"), catalog)
	if len(candidates) != 0 {
		t.Fatalf("expected zero candidates for flat token usage, got %d", len(candidates))
	}
}

// TestDetectTokenEfficiency_BelowRequestFloorExcluded checks the
// >=500-request-per-window floor from Part G.3.3: a model with plenty of
// drift but too little traffic in either window must not fire.
func TestDetectTokenEfficiency_BelowRequestFloorExcluded(t *testing.T) {
	catalog := tokenEffCatalog(t)
	now := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	currentEnd := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	currentStart := currentEnd.AddDate(0, 0, -tokenEffCurrentDays)
	baselineStart := currentStart.AddDate(0, 0, -tokenEffBaselineDays)

	var stats []DailyModelStat
	// Only 10 requests/day in both windows -- well under the 500 floor.
	for d := baselineStart; d.Before(currentStart); d = d.AddDate(0, 0, 1) {
		stats = append(stats, dailyStat(d, "gpt-4o-mini", 10, 1000, 200))
	}
	for d := currentStart; d.Before(currentEnd); d = d.AddDate(0, 0, 1) {
		stats = append(stats, dailyStat(d, "gpt-4o-mini", 10, 2000, 200))
	}

	candidates := DetectTokenEfficiency(stats, now, types.AppID("app-1"), catalog)
	if len(candidates) != 0 {
		t.Fatalf("expected zero candidates below the request floor, got %d", len(candidates))
	}
}

// TestDetectTokenEfficiency_UnknownModelExcluded checks that a model with
// no catalog entry never fabricates a cost impact (Part D.2).
func TestDetectTokenEfficiency_UnknownModelExcluded(t *testing.T) {
	catalog := tokenEffCatalog(t)
	now := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	currentEnd := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	currentStart := currentEnd.AddDate(0, 0, -tokenEffCurrentDays)
	baselineStart := currentStart.AddDate(0, 0, -tokenEffBaselineDays)

	var stats []DailyModelStat
	for d := baselineStart; d.Before(currentStart); d = d.AddDate(0, 0, 1) {
		stats = append(stats, dailyStat(d, "some-local-model", 1000, 1000, 200))
	}
	for d := currentStart; d.Before(currentEnd); d = d.AddDate(0, 0, 1) {
		stats = append(stats, dailyStat(d, "some-local-model", 1000, 2000, 200))
	}

	candidates := DetectTokenEfficiency(stats, now, types.AppID("app-1"), catalog)
	if len(candidates) != 0 {
		t.Fatalf("expected zero candidates for a model with no catalog price, got %d", len(candidates))
	}
}
