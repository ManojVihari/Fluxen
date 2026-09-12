package sim

import "time"

// CachingScenario replays requests chronologically through a simulated
// exact-match cache (Part G.4.2) — same key function as Part G.3.2's
// Repeated Request detector, given a TTL and an optional size cap.
type CachingScenario struct {
	TTL time.Duration
	// MaxEntries caps how many distinct cache keys can be held at once
	// (an LRU eviction policy); zero means unbounded.
	MaxEntries int
}

// CachingResult is Part G.4's required report shape for this scenario —
// note there is no "candidate model," a cache hit serves the exact same
// response so the model breakdown always mirrors what actually served
// the traffic.
type CachingResult struct {
	ReplayedRequests   int64
	AffectedRequests   int64 // cache hits — requests that would have been served from cache
	ActualCostMicro    int64
	SimulatedCostMicro int64
	HitRate            float64
	Assumptions        []string
}

const cachingAssumption = "A cache hit is assumed to return the identical response at zero additional cost. Real-world eviction, cold starts, and provider-side nondeterminism are not modeled."

// cacheEntry is one live key in the simulated LRU.
type cacheEntry struct {
	expiresAt time.Time
}

// SimulateCaching replays facts (must already be in chronological order —
// the caller's ReplayFacts query guarantees this) through a simulated
// LRU cache keyed by CacheKey, honoring TTL and, if set, a maximum entry
// count via least-recently-used eviction. This is a **true replay**
// (Part G.4: "a true replayed hit rate, not a formula-based estimate") —
// unlike the Repeated Request detector's TTL-sweep estimate (Part
// G.3.2), every request here is walked in order and the cache's actual
// state at that instant decides hit or miss.
//
// A request with no cache key (nil/empty — Fluxen only started computing
// cache keys as of Phase 4; historical requests from before that change
// have none) can never hit or be cached; it simply passes through as a
// miss that doesn't populate the cache.
func SimulateCaching(facts []ReplayFact, scenario CachingScenario) CachingResult {
	result := CachingResult{
		ReplayedRequests: int64(len(facts)),
		Assumptions:      []string{cachingAssumption},
	}

	cache := make(map[string]cacheEntry)
	lru := make([]string, 0, len(facts))

	for _, f := range facts {
		result.ActualCostMicro += f.CostMicro

		key := string(f.CacheKey)
		now := f.StartedAt

		if key == "" {
			result.SimulatedCostMicro += f.CostMicro
			continue
		}

		if entry, hit := cache[key]; hit && now.Before(entry.expiresAt) {
			result.AffectedRequests++
			touchLRU(&lru, key)
			continue // a hit costs nothing — the response is served from cache
		}

		result.SimulatedCostMicro += f.CostMicro
		cache[key] = cacheEntry{expiresAt: now.Add(scenario.TTL)}
		touchLRU(&lru, key)
		if scenario.MaxEntries > 0 && len(lru) > scenario.MaxEntries {
			evict := lru[0]
			lru = lru[1:]
			delete(cache, evict)
		}
	}

	if result.ReplayedRequests > 0 {
		result.HitRate = float64(result.AffectedRequests) / float64(result.ReplayedRequests)
	}
	return result
}

// touchLRU moves key to the most-recently-used end, appending it if new.
func touchLRU(lru *[]string, key string) {
	for i, k := range *lru {
		if k == key {
			*lru = append((*lru)[:i], (*lru)[i+1:]...)
			break
		}
	}
	*lru = append(*lru, key)
}
