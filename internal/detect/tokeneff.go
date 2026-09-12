package detect

import (
	"fmt"
	"time"

	"fluxen/pkg/pricing"
	"fluxen/pkg/types"
)

// TokenEfficiencyDetectorVersion is stamped on every candidate this
// detector produces.
const TokenEfficiencyDetectorVersion = "token-efficiency-2026-09-01"

// Part G.3.3's fixed windows and thresholds.
const (
	tokenEffBaselineDays    = 28
	tokenEffCurrentDays     = 7
	tokenEffBaselineGapDays = 7 // baseline ends 7 days ago, not "now"
	tokenEffMinRequests     = 500
	inputDriftThreshold     = 0.25
	outputDriftThreshold    = 0.35
	tokenEffMonthlyScale    = 30.0 / float64(tokenEffCurrentDays)
)

// DailyModelStat mirrors store.DailyModelStat — internal/detect doesn't
// import internal/store, so it declares its own identical shape and the
// caller (Runner) converts.
type DailyModelStat struct {
	Day             time.Time
	Model           string
	Requests        int64
	InputTokensSum  int64
	OutputTokensSum int64
}

// TokenEfficiencyEvidence is the evidence JSON stored on a token-
// efficiency opportunity (Part G.3.3: "daily mean-token sparkline,
// drift %, cost impact, change-point date").
type TokenEfficiencyEvidence struct {
	Dimension          string            `json:"dimension"` // "input" | "output"
	Model              string            `json:"model"`
	BaselineMeanTokens float64           `json:"baseline_mean_tokens"`
	CurrentMeanTokens  float64           `json:"current_mean_tokens"`
	DriftPct           float64           `json:"drift_pct"`
	ChangePointDate    string            `json:"change_point_date,omitempty"`
	DailySparkline     []DailyTokenPoint `json:"daily_sparkline"`
	BaselineRequests   int64             `json:"baseline_requests"`
	CurrentRequests    int64             `json:"current_requests"`
}

// DailyTokenPoint is one point of the evidence's sparkline — the current
// window's own daily mean, so the UI can chart exactly what drifted.
type DailyTokenPoint struct {
	Day        string  `json:"day"`
	MeanTokens float64 `json:"mean_tokens"`
}

// TokenEfficiencyRecommendation is deliberately thin: Part G.3.3 is
// explicit that V1 "never rewrites prompts" — the only recommendation is
// to go look at the change-point date, not an automated fix.
type TokenEfficiencyRecommendation struct {
	Action string `json:"action"` // "investigate_prompt_drift"
}

// DetectTokenEfficiency is the Token Efficiency detector (Part G.3.3) —
// a pure function of daily per-model stats. It segments each model's
// history into a 28-day baseline (ending 7 days ago) and a trailing
// 7-day current window, and reports input and/or output drift
// independently — a model can produce up to two candidates (one per
// dimension) if both clear their own threshold.
func DetectTokenEfficiency(stats []DailyModelStat, now time.Time, appID types.AppID, catalog *pricing.Catalog) []Candidate {
	now = now.UTC()
	currentEnd := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	currentStart := currentEnd.AddDate(0, 0, -tokenEffCurrentDays)
	baselineEnd := currentStart
	baselineStart := baselineEnd.AddDate(0, 0, -tokenEffBaselineDays)

	byModel := make(map[string][]DailyModelStat)
	for _, s := range stats {
		byModel[s.Model] = append(byModel[s.Model], s)
	}

	var out []Candidate
	for model, days := range byModel {
		var baseline, current []DailyModelStat
		for _, d := range days {
			if !d.Day.Before(baselineStart) && d.Day.Before(baselineEnd) {
				baseline = append(baseline, d)
			} else if !d.Day.Before(currentStart) && d.Day.Before(currentEnd) {
				current = append(current, d)
			}
		}

		baselineReq, baselineIn, baselineOut := sumDaily(baseline)
		currentReq, currentIn, currentOut := sumDaily(current)
		if baselineReq < tokenEffMinRequests || currentReq < tokenEffMinRequests {
			continue
		}

		baselineMeanIn := float64(baselineIn) / float64(baselineReq)
		currentMeanIn := float64(currentIn) / float64(currentReq)
		baselineMeanOut := float64(baselineOut) / float64(baselineReq)
		currentMeanOut := float64(currentOut) / float64(currentReq)

		price, hasPrice := catalog.Lookup(model)

		if c, ok := buildDriftCandidate(appID, model, "input", inputDriftThreshold,
			baselineMeanIn, currentMeanIn, baselineReq, currentReq, current,
			price, hasPrice, price.InputPerMTokMicro,
			baselineStart, currentEnd); ok {
			out = append(out, c)
		}
		if c, ok := buildDriftCandidate(appID, model, "output", outputDriftThreshold,
			baselineMeanOut, currentMeanOut, baselineReq, currentReq, current,
			price, hasPrice, price.OutputPerMTokMicro,
			baselineStart, currentEnd); ok {
			out = append(out, c)
		}
	}
	return out
}

func sumDaily(days []DailyModelStat) (requests, input, output int64) {
	for _, d := range days {
		requests += d.Requests
		input += d.InputTokensSum
		output += d.OutputTokensSum
	}
	return
}

func buildDriftCandidate(
	appID types.AppID, model, dimension string, threshold float64,
	baselineMean, currentMean float64, baselineReq, currentReq int64, currentDays []DailyModelStat,
	price pricing.ModelPrice, hasPrice bool, priceMicroPerMTok int64,
	windowStart, windowEnd time.Time,
) (Candidate, bool) {
	if baselineMean <= 0 || !hasPrice {
		return Candidate{}, false
	}
	drift := (currentMean - baselineMean) / baselineMean
	if drift < threshold {
		return Candidate{}, false
	}

	// cost_impact = (current_mean - baseline_mean) x current_requests_30d x price_per_token
	// (Part G.3.3), normalized to a 30-day month like every other
	// detector's headline dollar figure (Part G.6).
	currentRequests30d := float64(currentReq) * tokenEffMonthlyScale
	pricePerToken := float64(priceMicroPerMTok) / 1_000_000
	costImpact := round((currentMean - baselineMean) * currentRequests30d * pricePerToken)

	// The pct-based floor needs this dimension's total current spend as
	// its denominator; the aggregate token count x price is exact (unlike
	// reconstructing a per-day spend from a mean), and that's all a
	// denominator needs.
	totalTokens := currentMean * float64(currentReq)
	currentCostMonthly := round(totalTokens * pricePerToken * tokenEffMonthlyScale)

	var savingsPct float64
	if currentCostMonthly > 0 {
		savingsPct = float64(costImpact) / float64(currentCostMonthly)
	}
	if !CandidateMeetsSavingsFloor(Candidate{SavingsMicro: costImpact, SavingsPct: savingsPct}) {
		return Candidate{}, false
	}

	sparkline := make([]DailyTokenPoint, 0, len(currentDays))
	changePointDate := ""
	cumulative := 0.0
	for _, d := range currentDays {
		if d.Requests == 0 {
			continue
		}
		var mean float64
		if dimension == "input" {
			mean = float64(d.InputTokensSum) / float64(d.Requests)
		} else {
			mean = float64(d.OutputTokensSum) / float64(d.Requests)
		}
		sparkline = append(sparkline, DailyTokenPoint{Day: d.Day.Format("2006-01-02"), MeanTokens: mean})

		// Change-point: a simple CUSUM — the first day the cumulative
		// deviation from the baseline mean exceeds one full baseline-
		// day's worth of tokens. Part G.3.3 asks for "a first day where
		// a cumulative-deviation statistic crosses a threshold" without
		// pinning an exact test; this is a documented, defensible choice
		// (not a rewritten prompt, just a marker — Part G.3.3's own
		// framing), not the only valid one.
		cumulative += mean - baselineMean
		if changePointDate == "" && cumulative >= baselineMean {
			changePointDate = d.Day.Format("2006-01-02")
		}
	}

	confidence := tokenEfficiencyConfidence(drift, currentReq)

	evidence := TokenEfficiencyEvidence{
		Dimension: dimension, Model: model,
		BaselineMeanTokens: baselineMean, CurrentMeanTokens: currentMean, DriftPct: drift,
		ChangePointDate: changePointDate, DailySparkline: sparkline,
		BaselineRequests: baselineReq, CurrentRequests: currentReq,
	}
	recommendation := TokenEfficiencyRecommendation{Action: "investigate_prompt_drift"}

	pct := int(drift * 100)
	title := fmt.Sprintf("%s token usage for %s is up %d%%", dimension, model, pct)
	summary := fmt.Sprintf(
		"%s tokens per request for %s rose %d%% over the last 7 days versus the prior 28-day baseline (%.0f -> %.0f tokens).",
		capitalize(dimension), model, pct, baselineMean, currentMean,
	)

	projectedCostMonthly := currentCostMonthly - costImpact

	return Candidate{
		Kind: KindTokenEfficiency, Fingerprint: fingerprint(appID, KindTokenEfficiency, model, dimension),
		Title: title, Summary: summary,
		WindowStart: windowStart, WindowEnd: windowEnd, SampleRequests: currentReq,
		CurrentCostMicro: currentCostMonthly, ProjectedCostMicro: projectedCostMonthly,
		SavingsMicro: costImpact, SavingsPct: savingsPct,
		Confidence: confidence, ConfidenceScore: confidence.Score(),
		Evidence: evidence, Recommendation: recommendation,
		DetectorVersion: TokenEfficiencyDetectorVersion,
	}, true
}

func tokenEfficiencyConfidence(drift float64, sample int64) Confidence {
	switch {
	case drift >= 0.5 && sample >= 5000:
		return ConfidenceHigh
	case drift >= 0.3 && sample >= 1000:
		return ConfidenceMedium
	default:
		return ConfidenceLow
	}
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return string(s[0]-32) + s[1:]
}
