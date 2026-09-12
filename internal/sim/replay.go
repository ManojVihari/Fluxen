// Package sim replays real historical request facts through the same
// pricing logic production uses (Rule 9, Part G.4) to answer "what would
// have happened" — never against synthetic data, never touching
// production. Every scenario function here is pure: no I/O, no wall-clock
// reads — the caller (internal/api's simulation handler) owns fetching
// facts from Postgres and persisting the result, so this package's own
// logic is unit-testable against a hand-built fixture (Rule 8).
package sim

import (
	"time"

	"fluxen/pkg/types"
)

// ReplayFact mirrors store.ReplayFact — internal/sim doesn't import
// internal/store (it would be the only reason to), so it declares its
// own identical shape and the caller converts.
type ReplayFact struct {
	ID             string
	StartedAt      time.Time
	RequestedModel string
	Provider       string
	Model          string
	InputTokens    int
	OutputTokens   int
	CostMicro      int64
	CostStatus     types.CostStatus
	Status         string
	CacheKey       []byte
}

// MaxReplayedRequests is Part G.4's replay cap: "capped at 2M replayed
// requests per run; beyond that it samples uniformly and labels the
// result as sampled."
const MaxReplayedRequests = 2_000_000

// Cap applies the replay cap via uniform stride sampling, preserving the
// caller's ordering (facts are expected chronological). At V1 demo/
// early-adopter scale this path is rarely exercised, but it exists so a
// deployment that does accumulate that much history degrades to a
// labeled sample instead of an unbounded query.
func Cap(facts []ReplayFact) (capped []ReplayFact, sampled bool) {
	if len(facts) <= MaxReplayedRequests {
		return facts, false
	}
	stride := float64(len(facts)) / float64(MaxReplayedRequests)
	out := make([]ReplayFact, 0, MaxReplayedRequests)
	for i := 0.0; int(i) < len(facts); i += stride {
		out = append(out, facts[int(i)])
	}
	return out, true
}

// ProjectMonthly scales a window total to a 30-day month — the same
// normalization Phase 3's detector applies (Part G.6: a shorter window's
// totals scaled to 30 days is "Projected"), used for a simulation's
// projected_monthly_savings_micro field.
func ProjectMonthly(windowDays float64, amount int64) int64 {
	if windowDays <= 0 {
		return 0
	}
	return round(float64(amount) * 30.0 / windowDays)
}

func round(f float64) int64 {
	if f < 0 {
		return int64(f - 0.5)
	}
	return int64(f + 0.5)
}
