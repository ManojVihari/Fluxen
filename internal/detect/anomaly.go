package detect

import (
	"fmt"
	"sort"
	"time"

	"fluxen/pkg/types"
)

// TrafficAnomalyDetectorVersion is stamped on every candidate this
// detector produces.
const TrafficAnomalyDetectorVersion = "traffic-anomaly-2026-09-01"

// Part G.3.4's fixed windows and thresholds.
const (
	anomalyLookbackDays    = 28
	anomalyMinHistoryDays  = 14
	anomalyEvaluationHours = 48
	anomalyZThreshold      = 3.5
	anomalyMinRunHours     = 2
	anomalyHighCostZ       = 5.0
	anomalyHighErrorSpike  = 0.10 // 10 percentage points
	// minSlotSamples is the minimum number of historical observations a
	// given hour-of-week slot needs before its median/MAD is trusted at
	// all — below this, every bucket in that slot is treated as
	// unevaluable (z = 0) rather than flagged off a near-empty baseline.
	minSlotSamples = 3
	// madConsistencyScale converts a median absolute deviation into a
	// robust estimate of standard deviation, the standard constant for
	// data drawn from a normal distribution.
	madConsistencyScale = 1.4826
)

// AnomalyHourPoint is one hour of the evidence's series (Part G.3.4:
// "the hourly series with the anomalous buckets highlighted").
type AnomalyHourPoint struct {
	Bucket      string  `json:"bucket"`
	Requests    int64   `json:"requests"`
	CostMicro   int64   `json:"cost_micro"`
	TotalTokens int64   `json:"total_tokens"`
	ErrorRate   float64 `json:"error_rate"`
	ZScore      float64 `json:"z_score"`
}

// TrafficAnomalyEvidence is the evidence JSON stored on a traffic-anomaly
// opportunity. Unlike every other kind, it carries no cost/savings
// figures (Part G.3.4: "no savings number is attached to an anomaly") —
// only what happened and how unusual it was against the app's own
// seasonal baseline.
type TrafficAnomalyEvidence struct {
	Metric            string             `json:"metric"` // "requests" | "cost" | "tokens" | "error_rate"
	PeakZScore        float64            `json:"peak_z_score"`
	BaselineMedian    float64            `json:"baseline_median"`
	PeakValue         float64            `json:"peak_value"`
	ErrorRateSpikePct float64            `json:"error_rate_spike_pct"`
	HourlySeries      []AnomalyHourPoint `json:"hourly_series"`
}

// TrafficAnomalyRecommendation is deliberately not an optimization — Part
// G.3.4 frames this kind as "go look," not "do this": the action deep
// links to a (future) Requests investigation view, never an Apply flow.
type TrafficAnomalyRecommendation struct {
	Action string `json:"action"` // "investigate"
}

// HourlyStat is the minimal per-hour shape this detector reads —
// deliberately not store.HourlyStat, so this file's own pure logic can be
// unit-tested against a hand-built fixture without a database (Rule 8).
// internal/detect's Runner maps store.HourlyStat into this shape.
type HourlyStat struct {
	Bucket      time.Time
	Requests    int64
	Errors      int64
	TotalTokens int64
	CostMicro   int64
}

type anomalyMetrics struct {
	requests  float64
	cost      float64
	tokens    float64
	errorRate float64
}

// DetectTrafficAnomaly is the Traffic Anomaly detector (Part G.3.4) — a
// pure function of an hourly fact slice. It builds a seasonal baseline
// (median/MAD per hour-of-week, over a trailing 28-day lookback) and
// scores the trailing 48 hours against it, flagging any run of 2+
// consecutive hours whose robust z-score exceeds 3.5 on request count,
// cost, or token volume.
func DetectTrafficAnomaly(stats []HourlyStat, now time.Time, appID types.AppID) []Candidate {
	if len(stats) == 0 {
		return nil
	}
	sorted := make([]HourlyStat, len(stats))
	copy(sorted, stats)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Bucket.Before(sorted[j].Bucket) })

	hourEnd := now.UTC().Truncate(time.Hour)
	evaluationStart := hourEnd.Add(-anomalyEvaluationHours * time.Hour)
	baselineEnd := evaluationStart
	baselineStart := hourEnd.Add(-anomalyLookbackDays * 24 * time.Hour)

	if sorted[0].Bucket.After(hourEnd.Add(-anomalyMinHistoryDays * 24 * time.Hour)) {
		// Not enough history yet to trust a seasonal baseline at all
		// (Part G.3.4: "minimum 14 days history to run").
		return nil
	}

	byBucket := make(map[int64]HourlyStat, len(sorted))
	for _, s := range sorted {
		byBucket[s.Bucket.Unix()] = s
	}

	type slotSample struct{ requests, cost, tokens, errorRate float64 }
	bySlot := make(map[int][]slotSample)
	for _, s := range sorted {
		if s.Bucket.Before(baselineStart) || !s.Bucket.Before(baselineEnd) {
			continue
		}
		slot := hourOfWeek(s.Bucket)
		bySlot[slot] = append(bySlot[slot], slotSample{
			requests: float64(s.Requests), cost: float64(s.CostMicro), tokens: float64(s.TotalTokens),
			errorRate: errorRateOf(s),
		})
	}

	type slotBaseline struct {
		medianRequests, madRequests   float64
		medianCost, madCost           float64
		medianTokens, madTokens       float64
		medianErrorRate, madErrorRate float64
		sampleCount                   int
	}
	baselines := make(map[int]slotBaseline, len(bySlot))
	for slot, samples := range bySlot {
		if len(samples) < minSlotSamples {
			continue
		}
		reqs := make([]float64, len(samples))
		costs := make([]float64, len(samples))
		toks := make([]float64, len(samples))
		errs := make([]float64, len(samples))
		for i, sm := range samples {
			reqs[i], costs[i], toks[i], errs[i] = sm.requests, sm.cost, sm.tokens, sm.errorRate
		}
		medReq, madReq := medianAndMAD(reqs)
		medCost, madCost := medianAndMAD(costs)
		medTok, madTok := medianAndMAD(toks)
		medErr, madErr := medianAndMAD(errs)
		baselines[slot] = slotBaseline{
			medianRequests: medReq, madRequests: madReq,
			medianCost: medCost, madCost: madCost,
			medianTokens: medTok, madTokens: madTok,
			medianErrorRate: medErr, madErrorRate: madErr, sampleCount: len(samples),
		}
	}

	type hourResult struct {
		bucket                                time.Time
		metrics                               anomalyMetrics
		zRequests, zCost, zTokens, zErrorRate float64
		peakZ                                 float64
		peakMetric                            string
		peakValue                             float64
		peakBaselineMedian                    float64
		errorRateSpike                        float64
	}
	var results []hourResult
	for b := evaluationStart; b.Before(hourEnd); b = b.Add(time.Hour) {
		var s HourlyStat
		if v, ok := byBucket[b.Unix()]; ok {
			s = v
		} else {
			s = HourlyStat{Bucket: b}
		}
		slot := hourOfWeek(b)
		base, ok := baselines[slot]
		hr := hourResult{bucket: b, metrics: anomalyMetrics{
			requests: float64(s.Requests), cost: float64(s.CostMicro), tokens: float64(s.TotalTokens),
			errorRate: errorRateOf(s),
		}}
		if ok {
			hr.zRequests = robustZ(hr.metrics.requests, base.medianRequests, base.madRequests)
			hr.zCost = robustZ(hr.metrics.cost, base.medianCost, base.madCost)
			hr.zTokens = robustZ(hr.metrics.tokens, base.medianTokens, base.madTokens)
			hr.zErrorRate = robustZ(hr.metrics.errorRate, base.medianErrorRate, base.madErrorRate)
			hr.errorRateSpike = hr.metrics.errorRate - base.medianErrorRate

			hr.peakZ, hr.peakMetric, hr.peakValue, hr.peakBaselineMedian = pickPeak(
				hr.zRequests, hr.metrics.requests, base.medianRequests,
				hr.zCost, hr.metrics.cost, base.medianCost,
				hr.zTokens, hr.metrics.tokens, base.medianTokens,
				hr.zErrorRate, hr.metrics.errorRate, base.medianErrorRate,
			)
		}
		results = append(results, hr)
	}

	var candidates []Candidate
	i := 0
	for i < len(results) {
		if abs(results[i].peakZ) <= anomalyZThreshold {
			i++
			continue
		}
		start := i
		for i < len(results) && abs(results[i].peakZ) > anomalyZThreshold {
			i++
		}
		run := results[start:i]
		if len(run) < anomalyMinRunHours {
			continue
		}

		peak := run[0]
		for _, r := range run[1:] {
			if abs(r.peakZ) > abs(peak.peakZ) {
				peak = r
			}
		}
		maxErrorSpike := run[0].errorRateSpike
		maxCostZ := run[0].zCost
		var sampleRequests int64
		series := make([]AnomalyHourPoint, len(run))
		for idx, r := range run {
			series[idx] = AnomalyHourPoint{
				Bucket: r.bucket.Format(time.RFC3339), Requests: int64(r.metrics.requests),
				CostMicro: int64(r.metrics.cost), TotalTokens: int64(r.metrics.tokens),
				ErrorRate: r.metrics.errorRate, ZScore: r.peakZ,
			}
			sampleRequests += int64(r.metrics.requests)
			if r.errorRateSpike > maxErrorSpike {
				maxErrorSpike = r.errorRateSpike
			}
			if r.zCost > maxCostZ {
				maxCostZ = r.zCost
			}
		}

		severity := "medium"
		if maxCostZ > anomalyHighCostZ || maxErrorSpike > anomalyHighErrorSpike {
			severity = "high"
		}
		confidence := anomalyConfidence(abs(peak.peakZ), len(run))

		windowStart := run[0].bucket
		windowEnd := run[len(run)-1].bucket.Add(time.Hour)

		evidence := TrafficAnomalyEvidence{
			Metric: peak.peakMetric, PeakZScore: peak.peakZ, BaselineMedian: peak.peakBaselineMedian,
			PeakValue: peak.peakValue, ErrorRateSpikePct: maxErrorSpike, HourlySeries: series,
		}
		recommendation := TrafficAnomalyRecommendation{Action: "investigate"}

		title := fmt.Sprintf("Unusual %s activity detected", peak.peakMetric)
		summary := fmt.Sprintf(
			"%s over %d consecutive hours starting %s deviated sharply (z=%.1f) from this application's usual pattern for that time of week.",
			capitalize(peak.peakMetric), len(run), windowStart.Format("2006-01-02 15:04 MST"), peak.peakZ,
		)

		candidates = append(candidates, Candidate{
			Kind: KindTrafficAnomaly, Fingerprint: fingerprint(appID, KindTrafficAnomaly, peak.peakMetric, windowStart.Format(time.RFC3339)),
			Title: title, Summary: summary, Severity: &severity,
			WindowStart: windowStart, WindowEnd: windowEnd, SampleRequests: sampleRequests,
			Confidence: confidence, ConfidenceScore: confidence.Score(),
			Evidence: evidence, Recommendation: recommendation,
			DetectorVersion: TrafficAnomalyDetectorVersion,
		})
	}
	return candidates
}

func errorRateOf(s HourlyStat) float64 {
	if s.Requests == 0 {
		return 0
	}
	return float64(s.Errors) / float64(s.Requests)
}

// hourOfWeek buckets a timestamp into one of 168 (7*24) seasonal slots.
func hourOfWeek(t time.Time) int {
	return int(t.Weekday())*24 + t.Hour()
}

// medianAndMAD returns the median and the median absolute deviation
// (scaled by madConsistencyScale to be comparable to a standard
// deviation for normally-distributed data) of vals.
func medianAndMAD(vals []float64) (median, mad float64) {
	if len(vals) == 0 {
		return 0, 0
	}
	median = medianFloat(vals)
	deviations := make([]float64, len(vals))
	for i, v := range vals {
		deviations[i] = abs(v - median)
	}
	mad = medianFloat(deviations) * madConsistencyScale
	return median, mad
}

func medianFloat(vals []float64) float64 {
	sorted := make([]float64, len(vals))
	copy(sorted, vals)
	sort.Float64s(sorted)
	n := len(sorted)
	if n%2 == 1 {
		return sorted[n/2]
	}
	return (sorted[n/2-1] + sorted[n/2]) / 2
}

// robustZ is a median/MAD z-score. When MAD is 0 (a slot with no
// historical variance at all), any deviation from the median is treated
// as an infinite-strength signal only if the deviation itself is
// nonzero — otherwise it's exactly on-baseline and z is 0.
func robustZ(value, median, mad float64) float64 {
	if mad == 0 {
		if value == median {
			return 0
		}
		if value > median {
			return anomalyZThreshold + 1
		}
		return -(anomalyZThreshold + 1)
	}
	return (value - median) / mad
}

func pickPeak(zReq, vReq, bReq, zCost, vCost, bCost, zTok, vTok, bTok, zErr, vErr, bErr float64) (peakZ float64, metric string, value, baseline float64) {
	peakZ, metric, value, baseline = zReq, "requests", vReq, bReq
	if abs(zCost) > abs(peakZ) {
		peakZ, metric, value, baseline = zCost, "cost", vCost, bCost
	}
	if abs(zTok) > abs(peakZ) {
		peakZ, metric, value, baseline = zTok, "tokens", vTok, bTok
	}
	if abs(zErr) > abs(peakZ) {
		peakZ, metric, value, baseline = zErr, "error_rate", vErr, bErr
	}
	return
}

func abs(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}

// anomalyConfidence mirrors every other detector's tiering style (Part
// G.3.4 doesn't define its own bands) — a stronger, longer-lasting
// deviation earns higher confidence.
func anomalyConfidence(peakZ float64, runHours int) Confidence {
	switch {
	case peakZ >= 6 && runHours >= 3:
		return ConfidenceHigh
	case peakZ >= anomalyZThreshold:
		return ConfidenceMedium
	default:
		return ConfidenceLow
	}
}
