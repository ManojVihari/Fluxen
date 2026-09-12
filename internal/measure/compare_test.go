package measure

import "testing"

func TestCostPer1k(t *testing.T) {
	cases := []struct {
		name          string
		costMicro     int64
		requests      int64
		wantCostPer1k int64
	}{
		{"zero requests", 5000, 0, 0},
		{"exact division", 1_000_000, 1000, 1_000_000},
		{"rounds to nearest", 1_000_500, 1000, 1_000_500},
		{"rounds down under half", 999, 1000, 999},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := CostPer1k(tc.costMicro, tc.requests); got != tc.wantCostPer1k {
				t.Errorf("CostPer1k(%d, %d) = %d, want %d", tc.costMicro, tc.requests, got, tc.wantCostPer1k)
			}
		})
	}
}

func TestCompare_HandComputedSavings(t *testing.T) {
	// Baseline: 10,000 micro per 1k requests. Observed: 5,000 requests at
	// 30,000,000 micro total => 6,000,000 micro per 1k.
	c := Compare(10_000, 30_000_000, 5_000)

	if c.ObservedCostPer1kMicro != 6_000_000 {
		t.Errorf("expected observed cost per 1k of 6,000,000, got %d", c.ObservedCostPer1kMicro)
	}
	// Wait: baseline (10,000) is tiny compared to observed (6,000,000) in
	// this fixture — that's deliberately unrealistic to make the formula
	// easy to hand-verify, not a realistic scenario.
	wantSavings := int64((10_000 - 6_000_000) * 5_000 / 1000)
	if c.ActualSavingsMicro != wantSavings {
		t.Errorf("expected actual_savings_micro=%d, got %d", wantSavings, c.ActualSavingsMicro)
	}
	wantPct := 1 - float64(6_000_000)/float64(10_000)
	if diff := c.ActualPct - wantPct; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("expected actual_pct=%v, got %v", wantPct, c.ActualPct)
	}
}

func TestCompare_RealisticSavingsScenario(t *testing.T) {
	// Baseline: 10,000,000 micro per 1k requests ($10/1k). Observed:
	// 2,000 requests at 14,000,000 micro total => 7,000,000 micro
	// per 1k ($7/1k) — a genuine 30% reduction.
	c := Compare(10_000_000, 14_000_000, 2_000)

	if c.ObservedCostPer1kMicro != 7_000_000 {
		t.Fatalf("expected observed cost per 1k of 7,000,000, got %d", c.ObservedCostPer1kMicro)
	}
	wantSavings := int64((10_000_000 - 7_000_000) * 2_000 / 1000) // 6,000,000
	if c.ActualSavingsMicro != wantSavings {
		t.Errorf("expected actual_savings_micro=%d, got %d", wantSavings, c.ActualSavingsMicro)
	}
	if diff := c.ActualPct - 0.3; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("expected actual_pct=0.3, got %v", c.ActualPct)
	}
}

func TestCompare_ZeroBaselineNeverDividesByZero(t *testing.T) {
	c := Compare(0, 1000, 10)
	if c.ActualPct != 0 || c.ActualSavingsMicro != 0 {
		t.Errorf("expected a zero baseline to produce a zero comparison, got %+v", c)
	}
}

func TestCompare_RegressionProducesNegativeSavingsAndPct(t *testing.T) {
	// Baseline: 5,000,000 micro/1k. Observed: 1,000 requests at
	// 8,000,000,000 micro total => 8,000,000 micro/1k — cost went up.
	c := Compare(5_000_000, 8_000_000_000, 1_000)
	if c.ActualPct >= 0 {
		t.Errorf("expected a negative actual_pct for a regression, got %v", c.ActualPct)
	}
	if c.ActualSavingsMicro >= 0 {
		t.Errorf("expected negative actual_savings_micro for a regression, got %d", c.ActualSavingsMicro)
	}
}
