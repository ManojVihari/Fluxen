package detect

import (
	"testing"
	"time"

	"fluxen/pkg/types"
)

func repeatedFact(t time.Time, key string, costMicro int64) RepeatedRequestFact {
	return RepeatedRequestFact{StartedAt: t, CacheKey: []byte(key), CostMicro: costMicro, CostStatus: types.CostKnown, RequestedModel: "gpt-4o-mini"}
}

// TestDetectRepeatedRequest_DuplicateHeavyFixtureProducesOpportunity is
// the detector-quality "duplicate-heavy" fixture (Part K).
func TestDetectRepeatedRequest_DuplicateHeavyFixtureProducesOpportunity(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var facts []RepeatedRequestFact

	// 200 distinct requests, each repeated 4 more times within a minute
	// (well within even the shortest 5m TTL) — a strong, clean duplicate
	// signal: duplicate_rate = 800/1000 = 0.8.
	for i := 0; i < 200; i++ {
		key := "k" + string(rune('A'+i%26)) + string(rune('a'+i/26))
		for rep := 0; rep < 5; rep++ {
			facts = append(facts, repeatedFact(base.Add(time.Duration(i)*time.Hour+time.Duration(rep)*time.Second), key, 10_000))
		}
	}

	candidates := DetectRepeatedRequest(facts, base, base.AddDate(0, 0, 7), types.AppID("app-1"))
	if len(candidates) != 1 {
		t.Fatalf("expected exactly one candidate, got %d", len(candidates))
	}
	c := candidates[0]
	if c.Kind != KindRepeatedRequest {
		t.Errorf("expected kind repeated_request, got %q", c.Kind)
	}
	ev, ok := c.Evidence.(RepeatedRequestEvidence)
	if !ok {
		t.Fatalf("expected RepeatedRequestEvidence, got %T", c.Evidence)
	}
	if ev.DuplicateRequests != 800 || ev.TotalRequests != 1000 {
		t.Errorf("expected 800/1000 duplicates, got %d/%d", ev.DuplicateRequests, ev.TotalRequests)
	}
	if len(ev.TTLSweep) != 4 {
		t.Errorf("expected all 4 TTL sweep rows, got %d", len(ev.TTLSweep))
	}
	if c.SavingsMicro <= 0 {
		t.Error("expected positive savings")
	}
	rec, ok := c.Recommendation.(RepeatedRequestRecommendation)
	if !ok || rec.Action != "enable_caching" {
		t.Fatalf("expected an enable_caching recommendation, got %+v", c.Recommendation)
	}
}

// TestDetectRepeatedRequest_CleanFixtureProducesNothing is the second
// half of the release-blocking detector-quality check: traffic with no
// duplicates must produce zero opportunities.
func TestDetectRepeatedRequest_CleanFixtureProducesNothing(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var facts []RepeatedRequestFact
	for i := 0; i < 1000; i++ {
		key := "unique-" + string(rune(i))
		facts = append(facts, repeatedFact(base.Add(time.Duration(i)*time.Minute), key, 10_000))
	}

	candidates := DetectRepeatedRequest(facts, base, base.AddDate(0, 0, 7), types.AppID("app-1"))
	if len(candidates) != 0 {
		t.Fatalf("expected zero candidates for traffic with no duplicates, got %d", len(candidates))
	}
}

func TestDetectRepeatedRequest_RepeatSixDaysLaterMissesEveryTTL(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var facts []RepeatedRequestFact
	// Enough distinct keys to keep total sample large, plus one pair six
	// days apart — outside even the widest (24h) TTL.
	for i := 0; i < 100; i++ {
		key := "unique-" + string(rune(i))
		facts = append(facts, repeatedFact(base.Add(time.Duration(i)*time.Minute), key, 10_000))
	}
	facts = append(facts, repeatedFact(base, "far-apart", 10_000))
	facts = append(facts, repeatedFact(base.AddDate(0, 0, 6), "far-apart", 10_000))

	candidates := DetectRepeatedRequest(facts, base, base.AddDate(0, 0, 7), types.AppID("app-1"))
	// duplicate_rate = 1/102, comfortably below the 0.05 floor.
	if len(candidates) != 0 {
		t.Fatalf("expected zero candidates when the only duplicate is six days apart, got %d", len(candidates))
	}
}

func TestDetectRepeatedRequest_UnknownCostExcluded(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var facts []RepeatedRequestFact
	for i := 0; i < 10; i++ {
		f := repeatedFact(base.Add(time.Duration(i)*time.Second), "k", 10_000)
		f.CostStatus = types.CostUnknown
		facts = append(facts, f)
	}

	candidates := DetectRepeatedRequest(facts, base, base.AddDate(0, 0, 7), types.AppID("app-1"))
	if len(candidates) != 0 {
		t.Fatalf("expected unknown-cost requests to be excluded entirely (Part D.2), got %d candidates", len(candidates))
	}
}
