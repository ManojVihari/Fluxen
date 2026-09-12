package detect

import (
	"testing"
	"time"

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

func priceFact(t *testing.T, catalog *pricing.Catalog, model string, inputTokens, outputTokens int, extra types.WorkloadFeatures) RequestFact {
	t.Helper()
	usage := types.ResponseUsage{InputTokens: inputTokens, OutputTokens: outputTokens, TotalTokens: inputTokens + outputTokens}
	_, _, total, status := pricing.Calculate(catalog, model, usage)
	price, _ := catalog.Lookup(model)
	return RequestFact{
		RequestedModel: model, Provider: price.Provider,
		InputTokens: inputTokens, OutputTokens: outputTokens,
		HasTools: extra.HasTools, HasToolCalls: extra.HasToolCalls, HasImages: extra.HasImages, JSONMode: extra.JSONMode,
		CostMicro: int64(total), CostStatus: status,
	}
}

// eligibleShapedFact builds a request small enough to fit gpt-4o-mini's
// eligibility predicate (Part G.3.1): well under 60% of its context
// window, output under its p90/max-output cap, no tool calls.
func eligibleShapedFact(t *testing.T, catalog *pricing.Catalog, model string) RequestFact {
	return priceFact(t, catalog, model, 1500, 200, types.WorkloadFeatures{})
}

// ineligibleShapedFact builds a request that genuinely needs the premium
// model's larger context window.
func ineligibleShapedFact(t *testing.T, catalog *pricing.Catalog, model string) RequestFact {
	return priceFact(t, catalog, model, 90000, 800, types.WorkloadFeatures{})
}

func TestModelCostConfidence_Tiers(t *testing.T) {
	cases := []struct {
		name             string
		eligibleFraction float64
		sample           int64
		sameProvider     bool
		want             Confidence
	}{
		{"high: strong fraction, large sample, same provider", 0.65, 6000, true, ConfidenceHigh},
		{"cross-provider caps high down to medium", 0.65, 6000, false, ConfidenceMedium},
		{"medium: moderate fraction and sample", 0.45, 1200, true, ConfidenceMedium},
		{"low: fraction ok but sample too small for medium", 0.45, 500, true, ConfidenceLow},
		{"low: sample large but fraction too small", 0.35, 6000, true, ConfidenceLow},
		{"high needs sample >= 5000; 4999 falls to medium", 0.65, 4999, true, ConfidenceMedium},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := modelCostConfidence(tc.eligibleFraction, tc.sample, tc.sameProvider)
			if got != tc.want {
				t.Errorf("modelCostConfidence(%v, %v, %v) = %v, want %v", tc.eligibleFraction, tc.sample, tc.sameProvider, got, tc.want)
			}
		})
	}
}

func TestEligibilityReason(t *testing.T) {
	candidate := pricing.ModelPrice{
		ID: "candidate", Provider: "openai",
		ContextWindow: 100000, MaxOutput: 1000,
		Capabilities: []string{"tools", "streaming"},
	}

	tests := []struct {
		name         string
		fact         RequestFact
		effMaxOutput int
		wantEligible bool
		wantReason   string
	}{
		{"fits comfortably", RequestFact{InputTokens: 1000, OutputTokens: 100}, 1000, true, ""},
		{"context too large", RequestFact{InputTokens: 70000, OutputTokens: 100}, 1000, false, "context_too_large"},
		{"output too large", RequestFact{InputTokens: 1000, OutputTokens: 2000}, 1000, false, "output_too_large"},
		{"has tool calls", RequestFact{InputTokens: 1000, OutputTokens: 100, HasToolCalls: true}, 1000, false, "has_tool_calls"},
		{"vision unsupported", RequestFact{InputTokens: 1000, OutputTokens: 100, HasImages: true}, 1000, false, "vision_unsupported"},
		{"json schema unsupported", RequestFact{InputTokens: 1000, OutputTokens: 100, JSONMode: true}, 1000, false, "json_schema_unsupported"},
		{"tools supported, fine", RequestFact{InputTokens: 1000, OutputTokens: 100, HasTools: true}, 1000, true, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			reason, eligible := eligibilityReason(tc.fact, candidate, tc.effMaxOutput)
			if eligible != tc.wantEligible || (!eligible && reason != tc.wantReason) {
				t.Errorf("eligibilityReason(%+v) = (%q, %v), want (%q, %v)", tc.fact, reason, eligible, tc.wantReason, tc.wantEligible)
			}
		})
	}
}

// TestDetectModelCost_ExpensiveModelFixtureProducesOpportunity is the
// first entry in Part K's detector-quality suite (Part L Phase 3,
// release-blocking): a fixture with an injected model-cost inefficiency
// must produce exactly the expected opportunity.
func TestDetectModelCost_ExpensiveModelFixtureProducesOpportunity(t *testing.T) {
	catalog := testCatalog(t)
	windowStart, windowEnd := time.Now().AddDate(0, 0, -14), time.Now()

	var facts []RequestFact
	// 1300 eligible-shaped gpt-4o requests (didn't need the premium
	// model) and 700 genuinely-large gpt-4o requests — 65% eligible,
	// comfortably clearing the 30% reporting floor and the confidence
	// tiers' sample requirements.
	for i := 0; i < 1300; i++ {
		facts = append(facts, eligibleShapedFact(t, catalog, "gpt-4o"))
	}
	for i := 0; i < 700; i++ {
		facts = append(facts, ineligibleShapedFact(t, catalog, "gpt-4o"))
	}

	candidates := DetectModelCost(facts, catalog, windowStart, windowEnd, types.AppID("app-1"))

	if len(candidates) != 1 {
		t.Fatalf("expected exactly one candidate, got %d", len(candidates))
	}
	c := candidates[0]

	if c.Kind != "model_cost" {
		t.Errorf("expected kind model_cost, got %q", c.Kind)
	}
	ev, ok := c.Evidence.(ModelCostEvidence)
	if !ok {
		t.Fatalf("expected ModelCostEvidence, got %T", c.Evidence)
	}
	if ev.CurrentModel != "gpt-4o" || ev.CandidateModel != "gpt-4o-mini" {
		t.Errorf("expected gpt-4o -> gpt-4o-mini, got %s -> %s", ev.CurrentModel, ev.CandidateModel)
	}
	if ev.TotalRequests != 2000 || ev.EligibleRequests != 1300 {
		t.Errorf("expected 1300/2000 eligible, got %d/%d", ev.EligibleRequests, ev.TotalRequests)
	}
	wantFraction := 1300.0 / 2000.0
	if diff := ev.EligibleFraction - wantFraction; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("expected eligible fraction %v, got %v", wantFraction, ev.EligibleFraction)
	}
	if ev.Caveat == "" {
		t.Error("expected a non-empty no-quality-claim caveat")
	}

	rec, ok := c.Recommendation.(ModelCostRecommendation)
	if !ok {
		t.Fatalf("expected ModelCostRecommendation, got %T", c.Recommendation)
	}
	// Conservative split: min(eligible_fraction, 0.5) — eligible_fraction
	// here (0.65) exceeds 0.5, so the recommendation must cap at 0.5.
	if rec.RecommendedTrafficWeight != 0.5 {
		t.Errorf("expected conservative split to cap at 0.5, got %v", rec.RecommendedTrafficWeight)
	}

	if c.SavingsMicro <= 0 {
		t.Error("expected positive savings")
	}
	if c.ProjectedCostMicro != c.CurrentCostMicro-c.SavingsMicro {
		t.Errorf("expected projected = current - savings (%d != %d - %d)", c.ProjectedCostMicro, c.CurrentCostMicro, c.SavingsMicro)
	}
	if c.SavingsPct <= 0 || c.SavingsPct >= 1 {
		t.Errorf("expected a savings percentage in (0, 1), got %v", c.SavingsPct)
	}
	if c.Confidence != ConfidenceMedium {
		// sample=2000 clears the medium threshold (>=1000) but not
		// high's 5000, so this fixture's own numbers make medium the
		// only internally-consistent answer.
		t.Errorf("expected medium confidence for a 2000-sample fixture, got %v", c.Confidence)
	}
}

// TestDetectModelCost_CleanFixtureProducesNoOpportunities is the second
// half of the release-blocking detector-quality check: traffic with no
// injected inefficiency must produce zero opportunities, never a
// low-confidence guess.
func TestDetectModelCost_CleanFixtureProducesNoOpportunities(t *testing.T) {
	catalog := testCatalog(t)
	windowStart, windowEnd := time.Now().AddDate(0, 0, -14), time.Now()

	var facts []RequestFact
	// Every request already on the cheap model — there is no
	// gpt-4o traffic to find an opportunity in.
	for i := 0; i < 2000; i++ {
		facts = append(facts, eligibleShapedFact(t, catalog, "gpt-4o-mini"))
	}

	candidates := DetectModelCost(facts, catalog, windowStart, windowEnd, types.AppID("app-1"))
	if len(candidates) != 0 {
		t.Fatalf("expected zero candidates for clean traffic, got %d", len(candidates))
	}
}

// TestDetectModelCost_AllLargeContextProducesNothing checks the other
// clean case: gpt-4o traffic that genuinely needs the premium model
// (below the 30% eligible-fraction reporting floor) must not surface an
// opportunity either.
func TestDetectModelCost_AllLargeContextProducesNothing(t *testing.T) {
	catalog := testCatalog(t)
	windowStart, windowEnd := time.Now().AddDate(0, 0, -14), time.Now()

	var facts []RequestFact
	for i := 0; i < 2000; i++ {
		facts = append(facts, ineligibleShapedFact(t, catalog, "gpt-4o"))
	}

	candidates := DetectModelCost(facts, catalog, windowStart, windowEnd, types.AppID("app-1"))
	if len(candidates) != 0 {
		t.Fatalf("expected zero candidates when no traffic is eligible, got %d", len(candidates))
	}
}
