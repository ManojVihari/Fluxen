package measure

// CostPer1k computes cost per 1,000 requests — Part G.5: "Measurement
// compares cost per 1,000 requests, not raw totals, because request
// volume always changes." Requests==0 returns 0 rather than dividing —
// a window with no traffic has no rate to speak of.
func CostPer1k(costMicro, requests int64) int64 {
	if requests == 0 {
		return 0
	}
	return round(float64(costMicro) * 1000 / float64(requests))
}

// Comparison is baseline-vs-observed, computed via Part G.5's exact
// formulas — the same two numbers (baseline and observed cost-per-1k)
// drive both actual_savings_micro and actual_pct, so they can never
// disagree with each other about direction.
type Comparison struct {
	ObservedCostPer1kMicro int64
	ActualSavingsMicro     int64
	ActualPct              float64
}

// Compare applies Part G.5's formulas exactly:
//
//	actual_savings = (baseline_cost_per_1k − observed_cost_per_1k) × observed_requests / 1000
//	actual_pct     = 1 − observed_cost_per_1k / baseline_cost_per_1k
//
// A zero baseline_cost_per_1k (the application had no known-cost traffic
// in its pre-apply baseline — Part D.2 excludes unknown/local cost, so
// this is possible even with real request volume) makes actual_pct
// undefined; Compare returns 0 for both rather than dividing by zero,
// and the caller's verdict logic treats an undefined comparison the same
// as no effect.
func Compare(baselineCostPer1kMicro, observedCostMicro, observedRequests int64) Comparison {
	observedCostPer1k := CostPer1k(observedCostMicro, observedRequests)
	if baselineCostPer1kMicro == 0 {
		return Comparison{ObservedCostPer1kMicro: observedCostPer1k}
	}

	actualSavings := round(float64(baselineCostPer1kMicro-observedCostPer1k) * float64(observedRequests) / 1000)
	actualPct := 1 - float64(observedCostPer1k)/float64(baselineCostPer1kMicro)

	return Comparison{ObservedCostPer1kMicro: observedCostPer1k, ActualSavingsMicro: actualSavings, ActualPct: actualPct}
}

func round(f float64) int64 {
	if f < 0 {
		return int64(f - 0.5)
	}
	return int64(f + 0.5)
}
