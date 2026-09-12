package sim

import (
	"time"

	corepolicy "fluxen/pkg/policy"
)

// BudgetScenario is the third and last Part G.4 scenario type, deferred
// from Phase 4 to Phase 5 alongside the budget control it simulates
// (building it with no corresponding control to apply it to would have
// been simulation for its own sake).
type BudgetScenario struct {
	LimitMicro int64
	Period     string // "daily" | "monthly" — see pkg/policy.BudgetPeriodDaily/Monthly
}

// BudgetImpactResult is framed as impact, never as savings (Part G.4: "a
// budget is a control, not an optimization") — there is deliberately no
// savings/delta field here, only how much traffic would have been
// rejected and when.
type BudgetImpactResult struct {
	ReplayedRequests int64
	BlockedRequests  int64
	BlockedPct       float64
	// FirstBlockedDate/LastBlockedDate (YYYY-MM-DD, UTC) bound the range
	// blocking was concentrated in — nil if nothing would have been
	// blocked.
	FirstBlockedDate *string
	LastBlockedDate  *string
	Assumptions      []string
}

const budgetAssumption = "Requests are replayed in their original order and priced at their real recorded cost; a request that would have been blocked contributes nothing to the running period total, exactly as it would in production."

// SimulateBudget replays facts chronologically (the caller's ReplayFacts
// query already guarantees this order), accumulating spend per period
// bucket via the same pkg/policy.EvaluateBudget function
// internal/guard's live BudgetGuard calls — Rule 9: the simulation must
// never reimplement the enforcement decision, only replay it. A blocked
// request's own cost never joins the running total, matching what would
// really happen: a rejected call was never billed.
func SimulateBudget(facts []ReplayFact, scenario BudgetScenario) BudgetImpactResult {
	doc := &corepolicy.BudgetPolicy{Enabled: true, Mode: corepolicy.BudgetModeHard, LimitMicro: scenario.LimitMicro, Period: scenario.Period}

	result := BudgetImpactResult{ReplayedRequests: int64(len(facts)), Assumptions: []string{budgetAssumption}}
	spentByBucket := map[string]int64{}
	var firstBlocked, lastBlocked *time.Time

	for _, f := range facts {
		bucket := periodBucket(scenario.Period, f.StartedAt)
		spent := spentByBucket[bucket]

		if corepolicy.EvaluateBudget(doc, spent) {
			result.BlockedRequests++
			t := f.StartedAt
			if firstBlocked == nil {
				firstBlocked = &t
			}
			lastBlocked = &t
			continue
		}
		spentByBucket[bucket] = spent + f.CostMicro
	}

	if result.ReplayedRequests > 0 {
		result.BlockedPct = float64(result.BlockedRequests) / float64(result.ReplayedRequests)
	}
	if firstBlocked != nil {
		first := firstBlocked.UTC().Format("2006-01-02")
		last := lastBlocked.UTC().Format("2006-01-02")
		result.FirstBlockedDate = &first
		result.LastBlockedDate = &last
	}
	return result
}

func periodBucket(period string, t time.Time) string {
	t = t.UTC()
	if period == corepolicy.BudgetPeriodMonthly {
		return t.Format("200601")
	}
	return t.Format("20060102")
}
