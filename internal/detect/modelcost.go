package detect

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"time"

	"fluxen/pkg/pricing"
	"fluxen/pkg/types"
)

// ModelCostDetectorVersion is stamped on every candidate this detector
// produces (opportunities.detector_version) — bump it whenever the
// eligibility predicate, confidence tiering, or recommendation math
// changes, so a later re-detection can tell an old evidence shape from a
// new one.
const ModelCostDetectorVersion = "model-cost-2026-09-01"

// RequestFact is the minimal per-request shape the Model Cost detector
// reads — deliberately not the full requests row, so this file's own
// logic is unit-testable against a hand-built fixture without a database
// (Rule 8). internal/detect's Runner is what maps store.ModelCostFact
// into this shape.
type RequestFact struct {
	RequestedModel string
	Provider       string
	InputTokens    int
	OutputTokens   int
	HasTools       bool
	HasToolCalls   bool
	HasImages      bool
	JSONMode       bool
	CostMicro      int64
	CostStatus     types.CostStatus
}

// eligibleFractionFloor is the minimum share of a model's traffic that
// must plausibly fit a candidate before this is worth reporting at all
// (Part G.3.1: "report only if ≥ 0.30").
const eligibleFractionFloor = 0.30

// modelCostCaveat is the fixed no-quality-claim string every model-cost
// opportunity's evidence must carry verbatim (Part G.3.1, Rule 18).
const modelCostCaveat = "Fluxen does not evaluate output quality. Review the simulation, then consider applying to a portion of traffic and comparing results."

// monthlyScale projects the trailing-14-day detection window to a 30-day
// month (Part G.2's floors are named "_monthly_"; Part G.6 documents this
// exact normalization — a shorter window's totals scaled to 30 days — as
// "Projected").
const monthlyScale = 30.0 / 14.0

// ModelCostEvidence is the evidence JSON stored on a model-cost
// opportunity and rendered by the Optimizations detail page's Evidence
// panel (Part L Phase 3 frontend tasks).
type ModelCostEvidence struct {
	CurrentModel       string           `json:"current_model"`
	CandidateModel     string           `json:"candidate_model"`
	TotalRequests      int64            `json:"total_requests"`
	EligibleRequests   int64            `json:"eligible_requests"`
	EligibleFraction   float64          `json:"eligible_fraction"`
	ExclusionBreakdown map[string]int64 `json:"exclusion_breakdown"`
	InputTokensP50     int              `json:"input_tokens_p50"`
	InputTokensP95     int              `json:"input_tokens_p95"`
	OutputTokensP50    int              `json:"output_tokens_p50"`
	OutputTokensP95    int              `json:"output_tokens_p95"`
	PriceRatio         float64          `json:"price_ratio"` // candidate list price / current list price, blended input+output
	WindowDays         int              `json:"window_days"`
	Caveat             string           `json:"caveat"`
}

// ModelCostRecommendation is the recommendation JSON stored on a
// model-cost opportunity — shaped so Phase 5's apply flow can consume it
// directly without a schema change (Part L Phase 3 database tasks).
type ModelCostRecommendation struct {
	Action                   string  `json:"action"` // "route_to_cheaper_model"
	CurrentModel             string  `json:"current_model"`
	CandidateModel           string  `json:"candidate_model"`
	RecommendedTrafficWeight float64 `json:"recommended_traffic_weight"`
}

// DetectModelCost is the Model Cost detector (Part G.3.1) — a pure
// function of a fact slice and the pricing catalog. It groups facts by
// requested_model, considers only the catalog's declared
// downgrade_candidates_for as candidates, and returns one Candidate per
// (requested_model, candidate) pair that clears the eligible-fraction
// floor. Callers (Runner) still owe it Part G.2's app-level and
// savings-floor checks before persisting anything.
func DetectModelCost(facts []RequestFact, catalog *pricing.Catalog, windowStart, windowEnd time.Time, appID types.AppID) []Candidate {
	byModel := make(map[string][]RequestFact)
	for _, f := range facts {
		if f.CostStatus != types.CostKnown {
			continue // Part D.2: unknown/local cost is excluded from all savings math
		}
		byModel[f.RequestedModel] = append(byModel[f.RequestedModel], f)
	}

	var out []Candidate
	for model, group := range byModel {
		modelPrice, ok := catalog.Lookup(model)
		if !ok {
			continue
		}
		candidates := catalog.DowngradeCandidates(model)
		if len(candidates) == 0 {
			continue
		}

		outputTokens := make([]int, len(group))
		for i, f := range group {
			outputTokens[i] = f.OutputTokens
		}
		p90Output := percentile(outputTokens, 0.90)

		for _, cand := range candidates {
			c, ok := buildCandidate(group, modelPrice, cand, catalog, p90Output, windowStart, windowEnd, appID)
			if ok {
				out = append(out, c)
			}
		}
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Fingerprint < out[j].Fingerprint })
	return out
}

func buildCandidate(group []RequestFact, current, candidate pricing.ModelPrice, catalog *pricing.Catalog, p90Output int, windowStart, windowEnd time.Time, appID types.AppID) (Candidate, bool) {
	total := int64(len(group))
	exclusions := map[string]int64{}
	var eligible []RequestFact

	maxOutput := candidate.MaxOutput
	if p90Output < maxOutput {
		maxOutput = p90Output
	}

	for _, f := range group {
		reason, ok := eligibilityReason(f, candidate, maxOutput)
		if ok {
			eligible = append(eligible, f)
			continue
		}
		exclusions[reason]++
	}

	eligibleFraction := float64(len(eligible)) / float64(total)
	if eligibleFraction < eligibleFractionFloor {
		return Candidate{}, false
	}

	var eligibleCurrentCost, eligibleCandidateCost, currentCostAll int64
	for _, f := range group {
		currentCostAll += f.CostMicro
	}
	for _, f := range eligible {
		eligibleCurrentCost += f.CostMicro
		_, _, candTotal, _ := pricing.Calculate(catalog, candidate.ID, types.ResponseUsage{
			InputTokens: f.InputTokens, OutputTokens: f.OutputTokens, TotalTokens: f.InputTokens + f.OutputTokens,
		})
		eligibleCandidateCost += int64(candTotal)
	}

	eligibleSavings := eligibleCurrentCost - eligibleCandidateCost
	if eligibleSavings <= 0 {
		return Candidate{}, false
	}

	recommendedWeight := eligibleFraction
	if recommendedWeight > 0.5 {
		recommendedWeight = 0.5
	}
	scaleRatio := recommendedWeight / eligibleFraction

	savingsWindow := float64(eligibleSavings) * scaleRatio
	currentCostMonthly := float64(currentCostAll) * monthlyScale
	savingsMonthly := savingsWindow * monthlyScale
	projectedCostMonthly := currentCostMonthly - savingsMonthly

	var savingsPct float64
	if currentCostMonthly > 0 {
		savingsPct = savingsMonthly / currentCostMonthly
	}

	sameProvider := candidate.Provider == current.Provider
	confidence := modelCostConfidence(eligibleFraction, total, sameProvider)

	inputTokens := make([]int, len(group))
	for i, f := range group {
		inputTokens[i] = f.InputTokens
	}
	outputTokens := make([]int, len(group))
	for i, f := range group {
		outputTokens[i] = f.OutputTokens
	}

	priceRatio := float64(candidate.InputPerMTokMicro+candidate.OutputPerMTokMicro) / float64(current.InputPerMTokMicro+current.OutputPerMTokMicro)

	evidence := ModelCostEvidence{
		CurrentModel: current.ID, CandidateModel: candidate.ID,
		TotalRequests: total, EligibleRequests: int64(len(eligible)), EligibleFraction: eligibleFraction,
		ExclusionBreakdown: exclusions,
		InputTokensP50:     percentile(inputTokens, 0.50), InputTokensP95: percentile(inputTokens, 0.95),
		OutputTokensP50: percentile(outputTokens, 0.50), OutputTokensP95: percentile(outputTokens, 0.95),
		PriceRatio: priceRatio, WindowDays: 14, Caveat: modelCostCaveat,
	}
	recommendation := ModelCostRecommendation{
		Action: "route_to_cheaper_model", CurrentModel: current.ID, CandidateModel: candidate.ID,
		RecommendedTrafficWeight: recommendedWeight,
	}

	pct := int(eligibleFraction * 100)
	title := fmt.Sprintf("%d%% of %s traffic looks suitable for %s", pct, current.ID, candidate.ID)
	summary := fmt.Sprintf(
		"%d%% of recent requests on %s (%d of %d, over the last 14 days) match the token size and feature profile %s can handle.",
		pct, current.ID, len(eligible), total, candidate.ID,
	)

	return Candidate{
		Kind:           KindModelCost,
		Fingerprint:    fingerprint(appID, KindModelCost, current.ID, candidate.ID),
		Title:          title,
		Summary:        summary,
		WindowStart:    windowStart,
		WindowEnd:      windowEnd,
		SampleRequests: total,

		CurrentCostMicro:   round(currentCostMonthly),
		ProjectedCostMicro: round(projectedCostMonthly),
		SavingsMicro:       round(savingsMonthly),
		SavingsPct:         savingsPct,

		Confidence:      confidence,
		ConfidenceScore: confidence.Score(),

		Evidence:       evidence,
		Recommendation: recommendation,

		DetectorVersion: ModelCostDetectorVersion,
	}, true
}

// eligibilityReason applies Part G.3.1's eligibility predicate in the
// order stated there, returning the first failing check's reason so the
// evidence's exclusion breakdown can explain why a request didn't
// qualify.
func eligibilityReason(f RequestFact, candidate pricing.ModelPrice, effectiveMaxOutput int) (reason string, eligible bool) {
	if float64(f.InputTokens) > 0.6*float64(candidate.ContextWindow) {
		return "context_too_large", false
	}
	if f.OutputTokens > effectiveMaxOutput {
		return "output_too_large", false
	}
	if f.HasToolCalls {
		return "has_tool_calls", false
	}
	if f.HasTools && !candidate.HasCapability("tools") {
		return "tools_unsupported", false
	}
	if f.HasImages && !candidate.HasCapability("vision") {
		return "vision_unsupported", false
	}
	if f.JSONMode && !candidate.HasCapability("json_schema") {
		return "json_schema_unsupported", false
	}
	return "", true
}

// modelCostConfidence applies Part G.3.1's confidence tiering: high
// requires both a strong eligible fraction and a large same-provider
// sample; a cross-provider candidate can never report high regardless of
// how well it otherwise qualifies.
func modelCostConfidence(eligibleFraction float64, sample int64, sameProvider bool) Confidence {
	tier := ConfidenceLow
	switch {
	case eligibleFraction >= 0.6 && sample >= 5000:
		tier = ConfidenceHigh
	case eligibleFraction >= 0.4 && sample >= 1000:
		tier = ConfidenceMedium
	}
	if !sameProvider && tier == ConfidenceHigh {
		tier = ConfidenceMedium
	}
	return tier
}

// percentile returns the nearest-rank pth percentile (p in [0,1]) of vals.
// Returns 0 for an empty slice — callers only reach here with at least
// one request per model group.
func percentile(vals []int, p float64) int {
	if len(vals) == 0 {
		return 0
	}
	sorted := make([]int, len(vals))
	copy(sorted, vals)
	sort.Ints(sorted)
	idx := int(p*float64(len(sorted)-1) + 0.5)
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

func round(f float64) int64 {
	if f < 0 {
		return int64(f - 0.5)
	}
	return int64(f + 0.5)
}

func fingerprint(appID types.AppID, kind, model, candidate string) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%s|%s", kind, appID, model, candidate)))
	return hex.EncodeToString(sum[:])
}
