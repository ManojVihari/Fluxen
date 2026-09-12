package sim

import (
	"fluxen/pkg/pricing"
	"fluxen/pkg/types"
)

// ModelMixScenario reroutes a share of one model's traffic to a cheaper
// candidate (Part G.4.1) — the scenario type the Model Cost opportunity
// (Phase 3) simulates.
type ModelMixScenario struct {
	CurrentModel   string
	CandidateModel string
	// TrafficWeight is the fraction (0,1] of CurrentModel's matching
	// traffic to reroute — normally pre-filled from the opportunity's
	// own recommended_traffic_weight (Part G.3.1's conservative split).
	TrafficWeight float64
}

// ModelBreakdownRow is one model's actual-vs-simulated contribution.
// Requests means "how many requests this row's cost figures cover" —
// for CurrentModel that's every matching request before rerouting; for
// CandidateModel it's how many were actually rerouted to it.
type ModelBreakdownRow struct {
	Model              string `json:"model"`
	Requests           int64  `json:"requests"`
	ActualCostMicro    int64  `json:"actual_cost_micro"`
	SimulatedCostMicro int64  `json:"simulated_cost_micro"`
}

// ModelMixResult is Part G.4's required report shape for this scenario:
// current cost, simulated cost, delta, affected request count, per-model
// breakdown, and the fixed assumptions list.
type ModelMixResult struct {
	ReplayedRequests   int64
	AffectedRequests   int64
	ActualCostMicro    int64
	SimulatedCostMicro int64
	Breakdown          []ModelBreakdownRow
	Assumptions        []string
}

// modelMixAssumption is the fixed, verbatim assumption string Part G.4
// requires for every model-mix result.
const modelMixAssumption = "Token counts are assumed unchanged between models. Actual results may differ by roughly ±15%."

// SimulateModelMix replays facts through the model-mix scenario. Only
// requests actually served by CurrentModel with a known cost participate
// — Part D.2 excludes unknown/local cost from all savings math, and a
// request served by a different model isn't part of this scenario's
// population.
//
// Rerouting is chosen deterministically via even distribution across the
// matching population (a running fractional accumulator, not a stride
// index), so the same input always reroutes the same
// round(TrafficWeight × matched) requests regardless of how they're
// ordered — the closest discrete analogue to "reroute X% of traffic"
// that still yields a concrete, hand-verifiable affected-request count
// (Part G.4: "affected request count" is a required report field).
//
// Cost is always recomputed via pkg/pricing.Calculate — the exact same
// function the gateway uses (Rule 9) — never a reimplementation or a
// blended average.
func SimulateModelMix(facts []ReplayFact, scenario ModelMixScenario, catalog *pricing.Catalog) ModelMixResult {
	result := ModelMixResult{
		ReplayedRequests: int64(len(facts)),
		Assumptions:      []string{modelMixAssumption},
	}

	actualByModel := map[string]int64{}
	simulatedByModel := map[string]int64{}
	requestsByModel := map[string]int64{}

	acc := 0.0
	for _, f := range facts {
		// Same population the Model Cost detector reasons about (Part
		// G.3.1's ModelCostFacts query: status='ok', cost known) — a
		// simulation of that opportunity's own recommendation must agree
		// with the number the opportunity itself reported, not merely be
		// "close" (Rule 9: production and simulation must never diverge
		// on the same computation).
		if f.RequestedModel != scenario.CurrentModel || f.CostStatus != types.CostKnown || f.Status != "ok" {
			continue
		}

		result.ActualCostMicro += f.CostMicro
		actualByModel[scenario.CurrentModel] += f.CostMicro
		requestsByModel[scenario.CurrentModel]++

		acc += scenario.TrafficWeight
		if acc >= 1.0 {
			acc -= 1.0
			_, _, candTotal, _ := pricing.Calculate(catalog, scenario.CandidateModel, types.ResponseUsage{
				InputTokens: f.InputTokens, OutputTokens: f.OutputTokens, TotalTokens: f.InputTokens + f.OutputTokens,
			})
			result.SimulatedCostMicro += int64(candTotal)
			simulatedByModel[scenario.CandidateModel] += int64(candTotal)
			requestsByModel[scenario.CandidateModel]++
			result.AffectedRequests++
		} else {
			result.SimulatedCostMicro += f.CostMicro
			simulatedByModel[scenario.CurrentModel] += f.CostMicro
		}
	}

	for _, model := range []string{scenario.CurrentModel, scenario.CandidateModel} {
		if requestsByModel[model] == 0 && actualByModel[model] == 0 && simulatedByModel[model] == 0 {
			continue
		}
		result.Breakdown = append(result.Breakdown, ModelBreakdownRow{
			Model: model, Requests: requestsByModel[model],
			ActualCostMicro: actualByModel[model], SimulatedCostMicro: simulatedByModel[model],
		})
	}

	return result
}
