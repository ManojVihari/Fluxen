package trafficgen

import (
	"math/rand"
	"testing"
	"time"

	"fluxen/pkg/pricing"
	"fluxen/pkg/types"
)

func testOpts(days int) Options {
	return Options{
		Days: days,
		Now:  time.Date(2026, 3, 15, 12, 0, 0, 0, time.UTC),
		Rand: rand.New(rand.NewSource(42)),
	}
}

func TestGenerateRecords_ProducesRequestsAcrossEveryDay(t *testing.T) {
	catalog, err := pricing.LoadEmbedded()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	opts := testOpts(5)
	records := GenerateRecords(opts, "org-1", "app-1", catalog)

	if len(records) == 0 {
		t.Fatal("expected at least some records to be generated")
	}

	days := map[string]bool{}
	for _, r := range records {
		days[r.StartedAt.Format("2006-01-02")] = true
	}
	if len(days) != 6 { // Days=5 trailing days + today
		t.Errorf("expected records spanning 6 distinct days, got %d: %v", len(days), days)
	}
}

func TestGenerateRecords_OnlyUsesCatalogedModels(t *testing.T) {
	catalog, err := pricing.LoadEmbedded()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	records := GenerateRecords(testOpts(3), "org-1", "app-1", catalog)

	for _, r := range records {
		if r.Model != premiumModel && r.Model != cheapModel {
			t.Fatalf("unexpected model %q in generated records", r.Model)
		}
		if r.CostStatus != types.CostKnown {
			t.Fatalf("expected every generated record to have a known cost (both models are cataloged), got %v for model %q", r.CostStatus, r.Model)
		}
	}
}

func TestGenerateRecords_InjectsModelCostInefficiency(t *testing.T) {
	catalog, err := pricing.LoadEmbedded()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	records := GenerateRecords(testOpts(30), "org-1", "app-1", catalog)

	var premiumCount, eligibleOnPremium int
	for _, r := range records {
		if r.Model != premiumModel {
			continue
		}
		premiumCount++
		// Mirrors the eligibility shape used by generateOne: small
		// token counts and no tool calls.
		if r.InputTokens <= 3000 && r.OutputTokens <= 400 && !r.HasToolCalls {
			eligibleOnPremium++
		}
	}

	if premiumCount == 0 {
		t.Fatal("expected some requests on the premium model")
	}

	fraction := float64(eligibleOnPremium) / float64(premiumCount)
	// Target is 0.65; allow a wide tolerance since this is randomized —
	// the point of this test is "a real majority chunk is eligible," not
	// pinning the exact statistical constant.
	if fraction < 0.45 {
		t.Errorf("expected a meaningful fraction (~65%%) of premium-model requests to be cheap-model-eligible, got %.2f", fraction)
	}
}

func TestGenerateRecords_DeterministicForFixedSeed(t *testing.T) {
	catalog, err := pricing.LoadEmbedded()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	a := GenerateRecords(testOpts(3), "org-1", "app-1", catalog)
	b := GenerateRecords(testOpts(3), "org-1", "app-1", catalog)

	if len(a) != len(b) {
		t.Fatalf("expected the same record count for the same seed, got %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i].Model != b[i].Model || a[i].InputTokens != b[i].InputTokens || !a[i].StartedAt.Equal(b[i].StartedAt) {
			t.Fatalf("expected identical records at index %d for the same seed, got %+v vs %+v", i, a[i], b[i])
		}
	}
}

func TestGenerateRecords_SomeErrorsPresent(t *testing.T) {
	catalog, err := pricing.LoadEmbedded()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	records := GenerateRecords(testOpts(30), "org-1", "app-1", catalog)

	var errCount int
	for _, r := range records {
		if r.Status != "ok" {
			errCount++
		}
	}
	if errCount == 0 {
		t.Error("expected at least some non-ok requests over 30 days at a 2% error rate")
	}
}
