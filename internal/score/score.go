// Package score computes the application-level Efficiency Score (PRD
// §20: "Fluxen should provide an application-level Efficiency Score,"
// shown as a 0-100 headline with a breakdown of "reasons behind the
// score"; Implementation Plan Phase 7: "efficiency score, five weighted
// components, daily snapshot, 'not enough data' gate").
//
// Neither document freezes an exact formula or weighting — the PRD names
// five signals (cost efficiency, token efficiency, cache opportunity,
// model efficiency, traffic anomalies) and gives only a worked example of
// the shape, not the math. This package makes a deliberate, documented
// choice: reuse the same four detectors that power the Opportunities tab
// (Rule 9 — one source of truth for "how inefficient is this") rather
// than computing a second, parallel notion of inefficiency, so the
// score's own reasons are always traceable to evidence the user can
// independently inspect as an opportunity. Weights are equal (0.2 each)
// absent any stated priority among the five signals.
package score

import (
	"time"

	"fluxen/internal/detect"
	"fluxen/pkg/pricing"
	"fluxen/pkg/types"
)

// windowDays/monthlyScale intentionally mirror the same trailing-14-day
// window and 30-day projection every detector uses (Part G.2/G.6) — the
// score's dollar-based components must agree with the Opportunities tab
// on what "this app's monthly cost" means.
const (
	windowDays   = 14
	monthlyScale = 30.0 / 14.0
)

// componentWeight is each of the five components' share of the overall
// score (Implementation Plan Phase 7: "five weighted components"). Equal
// weighting is this package's own documented choice, not a value taken
// from the PRD or implementation plan.
const componentWeight = 0.2

// anomalyPenaltyMedium/High are the points a traffic-anomaly candidate
// subtracts from Traffic Stability — a high-severity anomaly (Part
// G.3.4: cost z > 5 or an error-rate spike > 10pp) costs twice what a
// medium one does, mirroring the same severity split the detector itself
// already makes.
const (
	anomalyPenaltyMedium = 20
	anomalyPenaltyHigh   = 40
)

// costTrendCeiling is the cost-per-request increase (as a fraction) at
// which the Cost Efficiency component bottoms out at 0 — a 50% increase
// in cost per request is treated as a complete loss of cost efficiency.
const costTrendCeiling = 0.5

// Components is the five-way breakdown Fluxen shows alongside the
// overall score (PRD §20's worked example).
type Components struct {
	ModelEfficiency  int
	TokenEfficiency  int
	CacheEfficiency  int
	TrafficStability int
	CostEfficiency   int
}

// Score is Compute's result: either a real 0-100 breakdown, or an
// explicit "not enough data" status — never a fabricated number for an
// application too new or too quiet to score (Part D.2's principle,
// applied here).
type Score struct {
	Status     string // "ok" | "insufficient_data"
	Overall    int
	Components Components
}

// Input is every fact Compute needs, already fetched — Compute itself
// touches no database (Rule 8: pure, unit-testable detection/scoring
// logic; internal/score's Runner is the only I/O).
type Input struct {
	AppID   types.AppID
	Now     time.Time
	Catalog *pricing.Catalog

	// Requests14D/Spend14DMicro/TotalCostMicro are the same trailing-14-day
	// totals detect.Runner reads via Rollups.Summary — Requests14D and
	// Spend14DMicro gate AppMeetsFloors, and TotalCostMicro (== the same
	// spend figure) is every dollar-based component's denominator.
	Requests14D    int64
	Spend14DMicro  int64
	TotalCostMicro int64

	ModelCostFacts       []detect.RequestFact
	RepeatedRequestFacts []detect.RepeatedRequestFact
	DailyModelStats      []detect.DailyModelStat
	HourlyStats          []detect.HourlyStat

	// Current/Prior request+cost totals over two equal-length, back-to-back
	// windows — the raw material for the Cost Efficiency trend component,
	// the one signal not already covered by the other four.
	CurrentRequests, CurrentCostMicro int64
	PriorRequests, PriorCostMicro     int64
}

// Compute is the Efficiency Score's pure entry point.
func Compute(in Input) Score {
	if !detect.AppMeetsFloors(in.Requests14D, in.Spend14DMicro) {
		// Part G.2's own floor, reused rather than reinvented: an
		// application too new or too quiet to trust a detector doesn't
		// deserve a trusted score either.
		return Score{Status: "insufficient_data"}
	}

	windowStart := in.Now.AddDate(0, 0, -windowDays)
	totalCostMonthly := int64(roundInt(float64(in.TotalCostMicro) * monthlyScale))

	modelCandidates := detect.DetectModelCost(in.ModelCostFacts, in.Catalog, windowStart, in.Now, in.AppID)
	modelSavings := bestSavingsPerSourceModel(modelCandidates)
	modelEfficiency := efficiencyFromSavings(modelSavings, totalCostMonthly)

	repeatedWindowStart := in.Now.AddDate(0, 0, -7)
	repeatedCandidates := detect.DetectRepeatedRequest(in.RepeatedRequestFacts, repeatedWindowStart, in.Now, in.AppID)
	cacheEfficiency := efficiencyFromSavings(sumSavings(repeatedCandidates), totalCostMonthly)

	tokenCandidates := detect.DetectTokenEfficiency(in.DailyModelStats, in.Now, in.AppID, in.Catalog)
	tokenEfficiency := efficiencyFromSavings(sumSavings(tokenCandidates), totalCostMonthly)

	anomalyCandidates := detect.DetectTrafficAnomaly(in.HourlyStats, in.Now, in.AppID)
	trafficStability := stabilityFromAnomalies(anomalyCandidates)

	costEfficiency := costTrendScore(in.CurrentRequests, in.CurrentCostMicro, in.PriorRequests, in.PriorCostMicro)

	overall := roundInt(componentWeight * float64(modelEfficiency+tokenEfficiency+cacheEfficiency+trafficStability+costEfficiency))

	return Score{
		Status: "ok", Overall: overall,
		Components: Components{
			ModelEfficiency: modelEfficiency, TokenEfficiency: tokenEfficiency, CacheEfficiency: cacheEfficiency,
			TrafficStability: trafficStability, CostEfficiency: costEfficiency,
		},
	}
}

// bestSavingsPerSourceModel sums the single best (highest-savings)
// candidate per requested_model — DetectModelCost can return several
// candidates for the same source model (one per declared downgrade
// target), and counting more than one would double-charge the same
// traffic's inefficiency against the score.
func bestSavingsPerSourceModel(candidates []detect.Candidate) int64 {
	best := make(map[string]int64)
	for _, c := range candidates {
		ev, ok := c.Evidence.(detect.ModelCostEvidence)
		if !ok {
			continue
		}
		if c.SavingsMicro > best[ev.CurrentModel] {
			best[ev.CurrentModel] = c.SavingsMicro
		}
	}
	var total int64
	for _, v := range best {
		total += v
	}
	return total
}

func sumSavings(candidates []detect.Candidate) int64 {
	var total int64
	for _, c := range candidates {
		total += c.SavingsMicro
	}
	return total
}

// efficiencyFromSavings converts an identified monthly savings figure
// into a 0-100 score: no identified savings is a perfect 100, and
// savings equal to (or exceeding) the app's entire monthly spend bottoms
// out at 0.
func efficiencyFromSavings(savingsMicro, totalCostMonthlyMicro int64) int {
	if totalCostMonthlyMicro <= 0 || savingsMicro <= 0 {
		return 100
	}
	ratio := float64(savingsMicro) / float64(totalCostMonthlyMicro)
	if ratio > 1 {
		ratio = 1
	}
	return roundInt(100 * (1 - ratio))
}

// stabilityFromAnomalies subtracts a fixed penalty per open anomaly run
// (Part G.3.4's own severity split), floored at 0 — Traffic Stability
// carries no dollar figure at all (an anomaly never has one), unlike the
// other four components.
func stabilityFromAnomalies(candidates []detect.Candidate) int {
	penalty := 0
	for _, c := range candidates {
		if c.Severity != nil && *c.Severity == "high" {
			penalty += anomalyPenaltyHigh
		} else {
			penalty += anomalyPenaltyMedium
		}
	}
	score := 100 - penalty
	if score < 0 {
		score = 0
	}
	return score
}

// costTrendScore is Cost Efficiency: the one component not derived from
// a detector, so the score reflects genuine cost-per-request drift even
// when nothing crosses any single detector's own reporting threshold. A
// flat or falling cost per request scores 100; a rise scores down to 0 at
// a 50% increase (costTrendCeiling).
func costTrendScore(currentRequests, currentCostMicro, priorRequests, priorCostMicro int64) int {
	if currentRequests == 0 || priorRequests == 0 {
		// Not enough history on one side of the comparison to trust a
		// trend — neutral, never a penalty for a young application.
		return 100
	}
	currentPerReq := float64(currentCostMicro) / float64(currentRequests)
	priorPerReq := float64(priorCostMicro) / float64(priorRequests)
	if priorPerReq <= 0 || currentPerReq <= priorPerReq {
		return 100
	}
	change := (currentPerReq - priorPerReq) / priorPerReq
	if change > costTrendCeiling {
		change = costTrendCeiling
	}
	return roundInt(100 * (1 - change/costTrendCeiling))
}

func roundInt(f float64) int {
	if f < 0 {
		return int(f - 0.5)
	}
	return int(f + 0.5)
}
