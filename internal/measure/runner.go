package measure

import (
	"context"
	"log/slog"
	"time"

	"fluxen/internal/policy"
	"fluxen/internal/store"
)

// InterimWindowDays/FinalWindowDays are Part G.5's fixed check points:
// "interim (+7d) and final (+14d) measurement checks."
const (
	InterimWindowDays = 7
	FinalWindowDays   = 14
)

// Runner is the only piece of internal/measure that touches Postgres:
// it finds every measurement still awaiting a check, runs whichever of
// interim/final is due, and persists the result. The comparison and
// verdict math stay pure (compare.go, verdict.go) and independently
// unit-testable, matching internal/detect's Runner/pure-logic split.
type Runner struct {
	Measurements  *store.Measurements
	Opportunities *store.Opportunities
	Requests      *store.Requests
	Policies      *policy.Store
	Logger        *slog.Logger

	// Now is injectable so fluxenctl's fast-forward affordance (Part L
	// Phase 6: "a clock-fast-forward affordance for testing without a
	// real two-week wait") and tests can run checks without waiting on
	// real time; nil means time.Now.
	Now func() time.Time
}

func NewRunner(measurements *store.Measurements, opportunities *store.Opportunities, requests *store.Requests, policies *policy.Store, logger *slog.Logger) *Runner {
	if logger == nil {
		logger = slog.Default()
	}
	return &Runner{Measurements: measurements, Opportunities: opportunities, Requests: requests, Policies: policies, Logger: logger}
}

func (r *Runner) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

// Run scans every measurement still awaiting a check and runs whichever
// of interim (+7d) / final (+14d) is now due, cascading through both in
// one tick if enough time has passed (a fast-forward can cross both
// thresholds at once). One measurement's failure is logged and skipped,
// never fatal to the rest (Part C.7's job contract).
func (r *Runner) Run(ctx context.Context) error {
	now := r.now().UTC()
	pending, err := r.Measurements.Pending(ctx, now)
	if err != nil {
		return err
	}

	for _, m := range pending {
		if m.Status == "collecting" && !now.Before(m.AppliedAt.AddDate(0, 0, InterimWindowDays)) {
			if err := r.checkInterim(ctx, m, now); err != nil {
				r.Logger.Error("measure: interim check failed", "measurement_id", m.ID, "error", err)
				continue
			}
		}

		// Re-fetch: the interim check above may have just landed, and if
		// +14d has also already passed (a fast-forward), the final check
		// should run in this same tick rather than waiting for the next one.
		current, err := r.Measurements.Get(ctx, m.OrgID, m.ID)
		if err != nil {
			r.Logger.Error("measure: failed to reload measurement", "measurement_id", m.ID, "error", err)
			continue
		}
		if current.Status == "interim" && !now.Before(m.AppliedAt.AddDate(0, 0, FinalWindowDays)) {
			if err := r.checkFinal(ctx, current, now); err != nil {
				r.Logger.Error("measure: final check failed", "measurement_id", m.ID, "error", err)
			}
		}
	}
	return nil
}

func (r *Runner) checkInterim(ctx context.Context, m store.Measurement, now time.Time) error {
	observedEnd := m.AppliedAt.AddDate(0, 0, InterimWindowDays)
	return r.check(ctx, m, m.AppliedAt, observedEnd, now, false)
}

func (r *Runner) checkFinal(ctx context.Context, m store.Measurement, now time.Time) error {
	observedEnd := m.AppliedAt.AddDate(0, 0, FinalWindowDays)
	return r.check(ctx, m, m.AppliedAt, observedEnd, now, true)
}

// check computes the observed window's real stats, compares them against
// the frozen baseline, determines a verdict (including confound
// detection via policy_history), and persists the result. observedStart/
// observedEnd are always the fixed, deterministic windows Part G.5
// defines (applied_at to +7d or +14d) — never "now" — so a delayed
// scheduler tick measures the same thing a prompt one would have.
func (r *Runner) check(ctx context.Context, m store.Measurement, observedStart, observedEnd, now time.Time, final bool) error {
	observedRequests, observedCostMicro, err := r.Requests.PeriodStats(ctx, m.AppID, observedStart, observedEnd)
	if err != nil {
		return err
	}

	comparison := Compare(m.BaselineCostPer1kMicro, observedCostMicro, observedRequests)

	confound, err := r.Policies.ChangedSince(ctx, m.AppID, m.PolicyVersion, observedEnd)
	if err != nil {
		return err
	}

	verdict, reason := DetermineVerdict(m.ExpectedPct, comparison.ActualPct, observedRequests, m.BaselineRequests, confound)

	if final {
		_, err = r.Measurements.RecordFinal(ctx, m.ID, now, observedStart, observedEnd, observedRequests, observedCostMicro,
			comparison.ObservedCostPer1kMicro, comparison.ActualSavingsMicro, comparison.ActualPct, string(verdict), reason)
	} else {
		_, err = r.Measurements.RecordInterim(ctx, m.ID, observedStart, observedEnd, observedRequests, observedCostMicro,
			comparison.ObservedCostPer1kMicro, comparison.ActualSavingsMicro, comparison.ActualPct, string(verdict), reason)
	}
	return err
}
