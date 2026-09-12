// Package detect turns raw request facts into opportunities — Fluxen's
// Aha Moment (Part L Phase 3): "Fluxen looked at MY application's AI
// traffic and found something I can actually optimize." A detector is a
// pure function of a fact slice (Rule 8: testable without a database);
// Runner is the only piece that touches Postgres, fetching facts and
// persisting whatever a detector returns after Part G.2's suppression
// rules are applied.
package detect

import "time"

// The four frozen detector kinds (Part G.3), matching the opportunities
// table's kind CHECK constraint exactly.
const (
	KindModelCost       = "model_cost"
	KindRepeatedRequest = "repeated_request"
	KindTokenEfficiency = "token_efficiency"
	KindTrafficAnomaly  = "traffic_anomaly"
)

// Confidence is the detector's own qualitative trust label (Part G.3.1).
type Confidence string

const (
	ConfidenceLow    Confidence = "low"
	ConfidenceMedium Confidence = "medium"
	ConfidenceHigh   Confidence = "high"
)

// Score maps a Confidence to the numeric weight Part G.2's ranking rule
// ("ranked by savings × confidence") uses — a fixed, documented mapping
// rather than a per-detector heuristic, so ranking behaves the same way
// regardless of which detector produced a candidate.
func (c Confidence) Score() float64 {
	switch c {
	case ConfidenceHigh:
		return 1.0
	case ConfidenceMedium:
		return 0.6
	default:
		return 0.3
	}
}

// Candidate is one detector's output before suppression, ranking, and
// persistence — the shape every detector kind produces so Runner can
// apply Part G.2's rules uniformly regardless of kind. Evidence and
// Recommendation are detector-specific and marshaled to JSON by the
// caller, never interpreted here.
type Candidate struct {
	Kind        string
	Fingerprint string
	Title       string
	Summary     string
	// Severity is only ever set by the traffic anomaly detector ("medium"
	// or "high", Part G.3.4) — nil for every other kind, matching
	// opportunities.severity's nullable, mostly-unused shape (Part E.1).
	Severity *string

	WindowStart    time.Time
	WindowEnd      time.Time
	SampleRequests int64

	// CurrentCostMicro/ProjectedCostMicro/SavingsMicro are projected to a
	// 30-day month (Part G.2's floors are named "_monthly_", and the
	// product's own worked example frames every dollar figure as
	// "/month" — Part G.6's precedent for this exact normalization, a
	// short window's totals scaled to 30 days, is "Projected"). The raw
	// 14-day window these are computed from is WindowStart/WindowEnd.
	CurrentCostMicro   int64
	ProjectedCostMicro int64
	SavingsMicro       int64
	SavingsPct         float64

	Confidence      Confidence
	ConfidenceScore float64

	Evidence       any
	Recommendation any

	DetectorVersion string
}
