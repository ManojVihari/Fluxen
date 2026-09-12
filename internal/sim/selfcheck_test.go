package sim

import "testing"

func TestSelfCheck_ReproducesRecordedCostExactly(t *testing.T) {
	catalog := testCatalog(t)

	var facts []ReplayFact
	for i := 0; i < 5; i++ {
		facts = append(facts, fact(t, catalog, "gpt-4o", 1000, 100))
	}
	for i := 0; i < 3; i++ {
		facts = append(facts, fact(t, catalog, "gpt-4o-mini", 2000, 200))
	}

	result := SelfCheck(facts, catalog)

	if !result.Pass {
		t.Fatalf("expected the self-check to pass reproducing its own fixture, got delta %.4f%%", result.DeltaPct*100)
	}
	if result.ActualCostMicro != result.ReplayedCostMicro {
		t.Errorf("expected exact reproduction (same pricing.Calculate call), got actual=%d replayed=%d", result.ActualCostMicro, result.ReplayedCostMicro)
	}
}

func TestSelfCheck_FailsIfRecordedCostDrifted(t *testing.T) {
	catalog := testCatalog(t)

	f := fact(t, catalog, "gpt-4o", 1000, 100)
	f.CostMicro = f.CostMicro * 2 // simulate a recorded cost that has drifted from what pricing.Calculate produces today

	result := SelfCheck([]ReplayFact{f}, catalog)

	if result.Pass {
		t.Fatal("expected the self-check to fail when recorded cost diverges from replay by more than 0.5%")
	}
}

func TestSelfCheck_UnknownModelReproducesZeroBothSides(t *testing.T) {
	result := SelfCheck([]ReplayFact{
		{Model: "not-a-real-model", InputTokens: 1000, OutputTokens: 100, CostMicro: 0},
	}, testCatalog(t))

	if !result.Pass {
		t.Errorf("expected zero-vs-zero to pass (Part D.2: unknown cost is always 0, never fabricated), got delta %.4f%%", result.DeltaPct*100)
	}
}
