package sim

import (
	"fluxen/pkg/pricing"
	"fluxen/pkg/types"
)

// SelfCheckThresholdPct is Part G.4's credibility floor: "replaying the
// currently applied policy over a past window must reproduce actual
// recorded cost within 0.5%."
const SelfCheckThresholdPct = 0.005

// SelfCheckResult reports whether replay reproduces recorded reality.
type SelfCheckResult struct {
	ReplayedRequests  int64
	ActualCostMicro   int64
	ReplayedCostMicro int64
	DeltaPct          float64
	Pass              bool
}

// SelfCheck is Part G.4's release-blocking determinism check. There is no
// policy engine yet (pkg/policy arrives in Phase 5) — every request in V1
// is served exactly as requested, with route_reason always "direct" —
// so "replaying the currently applied policy" for now means: recompute
// each request's cost from its own actually-served model and recorded
// token counts via pkg/pricing.Calculate — the exact function the
// gateway used to price it — and confirm the sum reproduces what was
// actually recorded. This is the credibility floor for the whole
// simulation feature: if replay can't even reproduce a no-op, its
// scenario results can't be trusted either. Once Phase 5 adds
// pkg/policy.Evaluate, this check extends to actually running the
// applied policy's routing decision, not just its pricing.
func SelfCheck(facts []ReplayFact, catalog *pricing.Catalog) SelfCheckResult {
	result := SelfCheckResult{ReplayedRequests: int64(len(facts))}

	for _, f := range facts {
		result.ActualCostMicro += f.CostMicro

		usage := types.ResponseUsage{InputTokens: f.InputTokens, OutputTokens: f.OutputTokens, TotalTokens: f.InputTokens + f.OutputTokens}
		_, _, total, _ := pricing.Calculate(catalog, f.Model, usage)
		result.ReplayedCostMicro += int64(total)
	}

	if result.ActualCostMicro != 0 {
		diff := result.ReplayedCostMicro - result.ActualCostMicro
		if diff < 0 {
			diff = -diff
		}
		result.DeltaPct = float64(diff) / float64(result.ActualCostMicro)
	}
	result.Pass = result.DeltaPct <= SelfCheckThresholdPct
	return result
}
