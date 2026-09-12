package score

import (
	"testing"
	"time"

	"fluxen/internal/detect"
	"fluxen/pkg/pricing"
	"fluxen/pkg/types"
)

func testCatalog(t *testing.T) *pricing.Catalog {
	t.Helper()
	c, err := pricing.LoadEmbedded()
	if err != nil {
		t.Fatalf("failed to load embedded catalog: %v", err)
	}
	return c
}

// gpt4oFact builds an eligible-shaped gpt-4o request (small enough to fit
// gpt-4o-mini's context/output caps, no tools/images) — the same shape
// internal/detect's own Model Cost fixtures use.
func gpt4oFact(t *testing.T, catalog *pricing.Catalog) detect.RequestFact {
	t.Helper()
	usage := types.ResponseUsage{InputTokens: 1500, OutputTokens: 200, TotalTokens: 1700}
	_, _, total, status := pricing.Calculate(catalog, "gpt-4o", usage)
	return detect.RequestFact{
		RequestedModel: "gpt-4o", Provider: "openai",
		InputTokens: 1500, OutputTokens: 200,
		CostMicro: int64(total), CostStatus: status,
	}
}

func baseInput(now time.Time, appID types.AppID, catalog *pricing.Catalog) Input {
	return Input{
		AppID: appID, Now: now, Catalog: catalog,
		Requests14D: 2000, Spend14DMicro: 20_000_000, TotalCostMicro: 20_000_000,
		CurrentRequests: 1000, CurrentCostMicro: 10_000_000,
		PriorRequests: 1000, PriorCostMicro: 10_000_000,
	}
}

func TestCompute_InsufficientDataReturnsInsufficientStatus(t *testing.T) {
	now := time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC)
	in := Input{AppID: types.AppID("app-1"), Now: now, Catalog: testCatalog(t), Requests14D: 10, Spend14DMicro: 1000}

	got := Compute(in)
	if got.Status != "insufficient_data" {
		t.Fatalf("expected insufficient_data status, got %q", got.Status)
	}
	if got.Overall != 0 {
		t.Errorf("expected zero overall for insufficient data, got %d", got.Overall)
	}
}

// TestCompute_CleanTrafficScoresPerfect is the release-blocking
// zero-false-positive check for scoring: an application with no detected
// inefficiency in any dimension and a flat cost trend must score 100
// across the board.
func TestCompute_CleanTrafficScoresPerfect(t *testing.T) {
	now := time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC)
	catalog := testCatalog(t)
	in := baseInput(now, types.AppID("app-1"), catalog)

	got := Compute(in)
	if got.Status != "ok" {
		t.Fatalf("expected ok status, got %q", got.Status)
	}
	if got.Overall != 100 {
		t.Errorf("expected overall 100, got %d (%+v)", got.Overall, got.Components)
	}
	want := Components{ModelEfficiency: 100, TokenEfficiency: 100, CacheEfficiency: 100, TrafficStability: 100, CostEfficiency: 100}
	if got.Components != want {
		t.Errorf("expected all components at 100, got %+v", got.Components)
	}
}

// TestCompute_ModelCostInefficiencyLowersModelEfficiencyOnly checks that
// a Model Cost-detector-shaped inefficiency pulls down ModelEfficiency
// (and therefore Overall) while leaving the other four components
// untouched.
func TestCompute_ModelCostInefficiencyLowersModelEfficiencyOnly(t *testing.T) {
	now := time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC)
	catalog := testCatalog(t)
	in := baseInput(now, types.AppID("app-1"), catalog)

	var facts []detect.RequestFact
	for i := 0; i < 1000; i++ {
		facts = append(facts, gpt4oFact(t, catalog))
	}
	in.ModelCostFacts = facts

	got := Compute(in)
	if got.Components.ModelEfficiency >= 100 {
		t.Errorf("expected ModelEfficiency below 100 for downgradeable gpt-4o traffic, got %d", got.Components.ModelEfficiency)
	}
	if got.Components.TokenEfficiency != 100 || got.Components.CacheEfficiency != 100 ||
		got.Components.TrafficStability != 100 || got.Components.CostEfficiency != 100 {
		t.Errorf("expected only ModelEfficiency to move, got %+v", got.Components)
	}
	if got.Overall >= 100 {
		t.Errorf("expected overall below 100, got %d", got.Overall)
	}
}

// TestCompute_TrafficAnomalyLowersTrafficStabilityOnly checks the one
// component with no dollar figure at all.
func TestCompute_TrafficAnomalyLowersTrafficStabilityOnly(t *testing.T) {
	now := time.Date(2026, 3, 15, 6, 0, 0, 0, time.UTC)
	catalog := testCatalog(t)
	in := baseInput(now, types.AppID("app-1"), catalog)

	hourEnd := now.UTC().Truncate(time.Hour)
	start := hourEnd.Add(-30 * 24 * time.Hour)
	var stats []detect.HourlyStat
	for b := start; b.Before(hourEnd); b = b.Add(time.Hour) {
		requests := int64(100)
		if !b.Before(hourEnd.Add(-6 * time.Hour)) {
			requests = 2000
		}
		stats = append(stats, detect.HourlyStat{Bucket: b, Requests: requests, TotalTokens: requests * 500, CostMicro: requests * 1000})
	}
	in.HourlyStats = stats

	got := Compute(in)
	if got.Components.TrafficStability >= 100 {
		t.Errorf("expected TrafficStability below 100 for a sustained spike, got %d", got.Components.TrafficStability)
	}
	if got.Components.ModelEfficiency != 100 || got.Components.TokenEfficiency != 100 || got.Components.CacheEfficiency != 100 {
		t.Errorf("expected only TrafficStability to move, got %+v", got.Components)
	}
}

// TestCompute_RisingCostPerRequestLowersCostEfficiencyOnly checks Cost
// Efficiency, the one component not derived from any detector.
func TestCompute_RisingCostPerRequestLowersCostEfficiencyOnly(t *testing.T) {
	now := time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC)
	catalog := testCatalog(t)
	in := baseInput(now, types.AppID("app-1"), catalog)
	// Cost per request doubled versus the prior window.
	in.CurrentRequests, in.CurrentCostMicro = 1000, 20_000_000
	in.PriorRequests, in.PriorCostMicro = 1000, 10_000_000

	got := Compute(in)
	if got.Components.CostEfficiency >= 100 {
		t.Errorf("expected CostEfficiency below 100 for a doubled cost-per-request, got %d", got.Components.CostEfficiency)
	}
	if got.Components.ModelEfficiency != 100 || got.Components.TokenEfficiency != 100 ||
		got.Components.CacheEfficiency != 100 || got.Components.TrafficStability != 100 {
		t.Errorf("expected only CostEfficiency to move, got %+v", got.Components)
	}
}
