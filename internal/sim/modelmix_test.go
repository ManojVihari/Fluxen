package sim

import (
	"testing"

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

func fact(t *testing.T, catalog *pricing.Catalog, model string, inputTokens, outputTokens int) ReplayFact {
	t.Helper()
	usage := types.ResponseUsage{InputTokens: inputTokens, OutputTokens: outputTokens, TotalTokens: inputTokens + outputTokens}
	_, _, total, status := pricing.Calculate(catalog, model, usage)
	return ReplayFact{
		RequestedModel: model, Model: model,
		InputTokens: inputTokens, OutputTokens: outputTokens,
		CostMicro: int64(total), CostStatus: status, Status: "ok",
	}
}

func TestSimulateModelMix_HandComputedFixture(t *testing.T) {
	catalog := testCatalog(t)

	// 10 identical gpt-4o requests (1000 in / 100 out each). Actual cost
	// per request: 1000*2.5 + 100*10 = 2500+1000 = 3500 micro.
	// Candidate (gpt-4o-mini) cost per request: 1000*0.15 + 100*0.6 = 150+60 = 210 micro.
	var facts []ReplayFact
	for i := 0; i < 10; i++ {
		facts = append(facts, fact(t, catalog, "gpt-4o", 1000, 100))
	}

	result := SimulateModelMix(facts, ModelMixScenario{
		CurrentModel: "gpt-4o", CandidateModel: "gpt-4o-mini", TrafficWeight: 0.5,
	}, catalog)

	if result.ReplayedRequests != 10 {
		t.Errorf("expected 10 replayed requests, got %d", result.ReplayedRequests)
	}
	if result.AffectedRequests != 5 {
		t.Errorf("expected exactly 5 affected (rerouted) requests at weight 0.5, got %d", result.AffectedRequests)
	}
	if result.ActualCostMicro != 35000 {
		t.Errorf("expected actual cost 35000 micro (10 * 3500), got %d", result.ActualCostMicro)
	}
	// 5 requests stay on gpt-4o (3500 each = 17500), 5 reroute to
	// gpt-4o-mini (210 each = 1050). Total = 18550.
	wantSimulated := int64(5*3500 + 5*210)
	if result.SimulatedCostMicro != wantSimulated {
		t.Errorf("expected simulated cost %d, got %d", wantSimulated, result.SimulatedCostMicro)
	}
	if len(result.Assumptions) != 1 || result.Assumptions[0] == "" {
		t.Error("expected the fixed ±15% assumption string to be present")
	}
}

func TestSimulateModelMix_IgnoresOtherModelsAndUnknownCost(t *testing.T) {
	catalog := testCatalog(t)

	facts := []ReplayFact{
		fact(t, catalog, "gpt-4o", 1000, 100),
		fact(t, catalog, "gpt-4o-mini", 1000, 100), // different model — not part of this scenario
		{RequestedModel: "gpt-4o", Model: "gpt-4o", InputTokens: 1000, OutputTokens: 100, CostMicro: 0, CostStatus: types.CostUnknown, Status: "ok"},
	}

	result := SimulateModelMix(facts, ModelMixScenario{
		CurrentModel: "gpt-4o", CandidateModel: "gpt-4o-mini", TrafficWeight: 1.0,
	}, catalog)

	if result.ReplayedRequests != 3 {
		t.Errorf("expected 3 replayed requests total, got %d", result.ReplayedRequests)
	}
	// Only the first fact (gpt-4o, known cost) is part of the scenario's population.
	if result.AffectedRequests != 1 {
		t.Errorf("expected exactly 1 affected request, got %d", result.AffectedRequests)
	}
	if result.ActualCostMicro != 3500 {
		t.Errorf("expected actual cost 3500 (only the one known-cost gpt-4o request), got %d", result.ActualCostMicro)
	}
}

func TestSimulateModelMix_FullWeightReroutesEveryMatchingRequest(t *testing.T) {
	catalog := testCatalog(t)
	var facts []ReplayFact
	for i := 0; i < 7; i++ {
		facts = append(facts, fact(t, catalog, "gpt-4o", 500, 50))
	}

	result := SimulateModelMix(facts, ModelMixScenario{
		CurrentModel: "gpt-4o", CandidateModel: "gpt-4o-mini", TrafficWeight: 1.0,
	}, catalog)

	if result.AffectedRequests != 7 {
		t.Errorf("expected all 7 requests rerouted at weight 1.0, got %d", result.AffectedRequests)
	}
}
