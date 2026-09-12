package sim

import (
	"testing"
	"time"
)

func cachedFact(startedAt time.Time, key string, costMicro int64) ReplayFact {
	return ReplayFact{
		StartedAt: startedAt, CacheKey: []byte(key), CostMicro: costMicro, Status: "ok",
	}
}

func TestSimulateCaching_HitWithinTTL(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	facts := []ReplayFact{
		cachedFact(base, "k1", 1000),
		cachedFact(base.Add(1*time.Minute), "k1", 1000), // repeat within TTL — should hit
	}

	result := SimulateCaching(facts, CachingScenario{TTL: time.Hour})

	if result.AffectedRequests != 1 {
		t.Fatalf("expected exactly 1 cache hit, got %d", result.AffectedRequests)
	}
	if result.ActualCostMicro != 2000 {
		t.Errorf("expected actual cost 2000 (both requests really happened), got %d", result.ActualCostMicro)
	}
	if result.SimulatedCostMicro != 1000 {
		t.Errorf("expected simulated cost 1000 (the hit costs nothing), got %d", result.SimulatedCostMicro)
	}
	if result.HitRate != 0.5 {
		t.Errorf("expected hit rate 0.5, got %v", result.HitRate)
	}
}

func TestSimulateCaching_MissAfterTTLExpires(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	facts := []ReplayFact{
		cachedFact(base, "k1", 1000),
		cachedFact(base.Add(2*time.Hour), "k1", 1000), // repeat six... well, 2h later — outside a 1h TTL
	}

	result := SimulateCaching(facts, CachingScenario{TTL: time.Hour})

	if result.AffectedRequests != 0 {
		t.Fatalf("expected zero hits once the TTL has expired, got %d", result.AffectedRequests)
	}
	if result.SimulatedCostMicro != 2000 {
		t.Errorf("expected simulated cost to equal actual cost when nothing hits, got %d", result.SimulatedCostMicro)
	}
}

func TestSimulateCaching_DifferentKeysNeverHit(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	facts := []ReplayFact{
		cachedFact(base, "k1", 1000),
		cachedFact(base.Add(time.Second), "k2", 1000),
	}

	result := SimulateCaching(facts, CachingScenario{TTL: time.Hour})

	if result.AffectedRequests != 0 {
		t.Fatalf("expected zero hits across different keys, got %d", result.AffectedRequests)
	}
}

func TestSimulateCaching_NoCacheKeyNeverHits(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	facts := []ReplayFact{
		{StartedAt: base, CacheKey: nil, CostMicro: 1000, Status: "ok"},
		{StartedAt: base.Add(time.Second), CacheKey: nil, CostMicro: 1000, Status: "ok"},
	}

	result := SimulateCaching(facts, CachingScenario{TTL: time.Hour})

	if result.AffectedRequests != 0 {
		t.Fatalf("expected requests with no cache key to never hit, got %d hits", result.AffectedRequests)
	}
	if result.SimulatedCostMicro != result.ActualCostMicro {
		t.Error("expected simulated cost to equal actual cost when no cache keys are present")
	}
}

func TestSimulateCaching_MaxEntriesEvictsLeastRecentlyUsed(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	facts := []ReplayFact{
		cachedFact(base, "k1", 100),
		cachedFact(base.Add(1*time.Second), "k2", 100),
		// k1 evicted here (MaxEntries=1, k2 is now the only live entry)
		cachedFact(base.Add(2*time.Second), "k1", 100), // would have hit without the cap — must miss
	}

	result := SimulateCaching(facts, CachingScenario{TTL: time.Hour, MaxEntries: 1})

	if result.AffectedRequests != 0 {
		t.Fatalf("expected the size cap to evict k1 before its repeat, got %d hits", result.AffectedRequests)
	}
}

func TestSimulateCaching_ThirdRepeatSixDaysLaterMissesA1HourTTL(t *testing.T) {
	// Mirrors Part G.3.2's own example: "a repeat six days later would
	// not hit a 1-hour cache."
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	facts := []ReplayFact{
		cachedFact(base, "k1", 500),
		cachedFact(base.AddDate(0, 0, 6), "k1", 500),
	}

	result := SimulateCaching(facts, CachingScenario{TTL: time.Hour})

	if result.AffectedRequests != 0 {
		t.Fatalf("expected zero hits for a repeat six days later against a 1-hour TTL, got %d", result.AffectedRequests)
	}
}
