package detect

import "sort"

// Suppression and trust floors (Part G.2, frozen). The global multiplier
// Part G.2 mentions ("a Settings-level knob, default 1.0") is a later
// phase's Settings feature — these are the raw defaults it would scale.
const (
	MinAppRequests14D          = 1_000
	MinAppSpend14DMicro        = 5_000_000  // $5
	MinSavingsMonthlyMicro     = 10_000_000 // $10
	MinSavingsPct              = 0.05
	MaxOpenOpportunitiesPerApp = 5
)

// AppMeetsFloors reports whether an application has enough trailing-14-day
// traffic and spend to run detectors against at all. Below this floor the
// correct product behavior is "Not enough data yet", never a
// low-confidence guess (Part G.2).
func AppMeetsFloors(requests14d int64, spend14dMicro int64) bool {
	return requests14d >= MinAppRequests14D && spend14dMicro >= MinAppSpend14DMicro
}

// CandidateMeetsSavingsFloor reports whether a computed candidate clears
// the minimum savings floor to actually be persisted. Candidates below
// this floor are still computed — by detectors themselves, for the
// detector-quality self-check tests (Part K) — this function is the only
// gate on whether they reach the opportunities table.
//
// Traffic anomaly candidates are exempt (Part G.3.4: "No savings number
// is attached to an anomaly") — a savings floor is meaningless for a
// kind that never reports one; anomalies pass straight through to
// ranking/capping instead.
func CandidateMeetsSavingsFloor(c Candidate) bool {
	if c.Kind == KindTrafficAnomaly {
		return true
	}
	return c.SavingsMicro >= MinSavingsMonthlyMicro && c.SavingsPct >= MinSavingsPct
}

// RankAndCap sorts candidates by savings × confidence (Part G.2) and caps
// the result to MaxOpenOpportunitiesPerApp — the rest are suppressed, not
// written, per application.
func RankAndCap(candidates []Candidate) []Candidate {
	ranked := make([]Candidate, len(candidates))
	copy(ranked, candidates)
	sort.SliceStable(ranked, func(i, j int) bool {
		return rankScore(ranked[i]) > rankScore(ranked[j])
	})
	if len(ranked) > MaxOpenOpportunitiesPerApp {
		ranked = ranked[:MaxOpenOpportunitiesPerApp]
	}
	return ranked
}

func rankScore(c Candidate) float64 {
	return float64(c.SavingsMicro) * c.ConfidenceScore
}
