package sim

import (
	"testing"
	"time"

	corepolicy "fluxen/pkg/policy"
)

func budgetFact(startedAt time.Time, costMicro int64) ReplayFact {
	return ReplayFact{StartedAt: startedAt, CostMicro: costMicro, Status: "ok"}
}

func TestSimulateBudget_BlocksOnceLimitReachedWithinPeriod(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	facts := []ReplayFact{
		budgetFact(base, 400),
		budgetFact(base.Add(time.Hour), 400),
		budgetFact(base.Add(2*time.Hour), 400), // pushes cumulative to 1200, over the 1000 limit... but check happens BEFORE adding
	}

	result := SimulateBudget(facts, BudgetScenario{LimitMicro: 1000, Period: corepolicy.BudgetPeriodDaily})

	// Request 1: spent=0 < 1000, allowed, spent -> 400.
	// Request 2: spent=400 < 1000, allowed, spent -> 800.
	// Request 3: spent=800 < 1000, allowed, spent -> 1200.
	if result.BlockedRequests != 0 {
		t.Fatalf("expected zero blocked requests since spend never reached the limit before a request, got %d", result.BlockedRequests)
	}

	// A 4th request now finds spent=1200 >= 1000 -> blocked.
	facts = append(facts, budgetFact(base.Add(3*time.Hour), 400))
	result = SimulateBudget(facts, BudgetScenario{LimitMicro: 1000, Period: corepolicy.BudgetPeriodDaily})
	if result.BlockedRequests != 1 {
		t.Fatalf("expected exactly 1 blocked request, got %d", result.BlockedRequests)
	}
	if result.ReplayedRequests != 4 {
		t.Errorf("expected 4 replayed requests, got %d", result.ReplayedRequests)
	}
	if result.BlockedPct != 0.25 {
		t.Errorf("expected blocked pct 0.25, got %v", result.BlockedPct)
	}
}

func TestSimulateBudget_BlockedRequestNeverAddsToSpend(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	facts := []ReplayFact{
		budgetFact(base, 1000),                  // hits the limit exactly
		budgetFact(base.Add(time.Hour), 5000),   // blocked — must not push spend further
		budgetFact(base.Add(2*time.Hour), 5000), // also blocked, same reason
	}

	result := SimulateBudget(facts, BudgetScenario{LimitMicro: 1000, Period: corepolicy.BudgetPeriodDaily})
	if result.BlockedRequests != 2 {
		t.Fatalf("expected 2 blocked requests, got %d", result.BlockedRequests)
	}
}

func TestSimulateBudget_PeriodsResetTheCounter(t *testing.T) {
	day1 := time.Date(2026, 1, 1, 23, 0, 0, 0, time.UTC)
	day2 := time.Date(2026, 1, 2, 1, 0, 0, 0, time.UTC)
	facts := []ReplayFact{
		budgetFact(day1, 900),
		budgetFact(day1.Add(30*time.Minute), 900), // day1: spent=900 < 1000, allowed; spent -> 1800
		budgetFact(day2, 900),                     // new day: spent=0 < 1000, allowed regardless of day1's total
	}

	result := SimulateBudget(facts, BudgetScenario{LimitMicro: 1000, Period: corepolicy.BudgetPeriodDaily})
	if result.BlockedRequests != 0 {
		t.Fatalf("expected the daily period to reset the counter across the day boundary, got %d blocked", result.BlockedRequests)
	}
}

func TestSimulateBudget_ReportsBlockedDateRange(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	facts := []ReplayFact{
		budgetFact(base, 1000),
		budgetFact(base.Add(time.Hour), 500), // blocked, same day
	}

	result := SimulateBudget(facts, BudgetScenario{LimitMicro: 1000, Period: corepolicy.BudgetPeriodDaily})
	if result.FirstBlockedDate == nil || result.LastBlockedDate == nil {
		t.Fatal("expected a blocked date range to be reported")
	}
	if *result.FirstBlockedDate != "2026-01-01" || *result.LastBlockedDate != "2026-01-01" {
		t.Errorf("expected both dates to be 2026-01-01, got %s / %s", *result.FirstBlockedDate, *result.LastBlockedDate)
	}
}

func TestSimulateBudget_NoBlocksReportsNilDateRange(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	result := SimulateBudget([]ReplayFact{budgetFact(base, 10)}, BudgetScenario{LimitMicro: 1_000_000, Period: corepolicy.BudgetPeriodDaily})
	if result.FirstBlockedDate != nil || result.LastBlockedDate != nil {
		t.Error("expected no blocked date range when nothing is blocked")
	}
}
