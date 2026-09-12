// Package measure closes the loop between what Fluxen estimated and what
// actually happened (Part L Phase 6): freeze a baseline at apply time,
// compare it against what was actually observed, and report an honest,
// volume-adjusted verdict. Every dollar figure here is either "measured"
// (baseline/observed, straight from real requests) or "realized" (the
// outcome of a completed verdict) — never blended with an opportunity's
// or simulation's "estimated"/"projected" figures (Part G.6).
package measure

import "math"

// Verdict is Part G.5's five-way outcome.
type Verdict string

const (
	VerdictSuccessful   Verdict = "successful"
	VerdictPartial      Verdict = "partial"
	VerdictNoEffect     Verdict = "no_effect"
	VerdictRegressed    Verdict = "regressed"
	VerdictInconclusive Verdict = "inconclusive"
)

// MinObservedFraction is Part G.5's inconclusive gate: observed requests
// below 30% of baseline requests means there isn't enough volume yet to
// trust the comparison.
const MinObservedFraction = 0.30

// NoEffectBand is Part G.5's ±2% band: a change this small is noise, not
// a real effect either way.
const NoEffectBand = 0.02

// DetermineVerdict applies Part G.5's frozen verdict table. The table's
// bands can overlap in principle (a tiny expected_pct would trivially
// satisfy "actual_pct >= 0.7 * expected_pct" even for a negligible real
// effect), so precedence matters and isn't stated explicitly in the
// spec; this resolves it the only way that keeps every band mutually
// exclusive: the data-quality gates (confound, low volume) first, then
// regressed/no_effect (both about the sign and size of actual_pct alone,
// independent of what was expected), and only then successful/partial
// (which compare actual_pct against expected_pct) — a real regression or
// a no-op is never reclassified as "successful" just because the
// original estimate happened to be tiny.
func DetermineVerdict(expectedPct, actualPct float64, observedRequests, baselineRequests int64, confound bool) (Verdict, string) {
	if confound {
		return VerdictInconclusive, "a second policy change landed inside the measurement window"
	}
	if baselineRequests > 0 && float64(observedRequests) < MinObservedFraction*float64(baselineRequests) {
		return VerdictInconclusive, "observed requests are below 30% of baseline requests — not enough volume yet to trust the comparison"
	}

	switch {
	case actualPct < -NoEffectBand:
		return VerdictRegressed, "cost per 1,000 requests increased by more than 2% since applying"
	case math.Abs(actualPct) < NoEffectBand:
		return VerdictNoEffect, "cost per 1,000 requests changed by less than 2% — no meaningful effect either way"
	case expectedPct > 0 && actualPct >= 0.7*expectedPct:
		return VerdictSuccessful, "realized savings reached at least 70% of the estimate"
	case expectedPct > 0 && actualPct >= 0.2*expectedPct:
		return VerdictPartial, "realized savings reached between 20% and 70% of the estimate"
	default:
		return VerdictInconclusive, "the realized effect does not clearly match any verdict band"
	}
}
