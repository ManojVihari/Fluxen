package detect

import (
	"testing"
	"time"

	"fluxen/pkg/types"
)

// hourlyFixture builds `days` days of hourly stats ending at `now`
// (exclusive), with a steady per-hour-of-week baseline: `requests`
// requests/hour, `costPerReq` micro-USD/request, `tokensPerReq`
// tokens/request, and zero errors — the same shape every hour of every
// day, so the seasonal baseline is flat and predictable in tests.
func hourlyFixture(now time.Time, days int, requests int64, costPerReq, tokensPerReq int64) []HourlyStat {
	hourEnd := now.UTC().Truncate(time.Hour)
	start := hourEnd.Add(-time.Duration(days) * 24 * time.Hour)
	var out []HourlyStat
	for b := start; b.Before(hourEnd); b = b.Add(time.Hour) {
		out = append(out, HourlyStat{
			Bucket: b, Requests: requests, Errors: 0,
			TotalTokens: requests * tokensPerReq, CostMicro: requests * costPerReq,
		})
	}
	return out
}

// TestDetectTrafficAnomaly_CleanFixtureProducesNothing is the
// release-blocking zero-false-positive check: perfectly flat traffic
// across the whole lookback window must never fire.
func TestDetectTrafficAnomaly_CleanFixtureProducesNothing(t *testing.T) {
	now := time.Date(2026, 3, 15, 6, 0, 0, 0, time.UTC)
	stats := hourlyFixture(now, 30, 100, 1000, 500)

	candidates := DetectTrafficAnomaly(stats, now, types.AppID("app-1"))
	if len(candidates) != 0 {
		t.Fatalf("expected zero candidates for flat traffic, got %d: %+v", len(candidates), candidates)
	}
}

// TestDetectTrafficAnomaly_InsufficientHistoryProducesNothing checks the
// minimum-14-days-history gate (Part G.3.4): a young application must
// get "not enough data," never a guess.
func TestDetectTrafficAnomaly_InsufficientHistoryProducesNothing(t *testing.T) {
	now := time.Date(2026, 3, 15, 6, 0, 0, 0, time.UTC)
	stats := hourlyFixture(now, 5, 100, 1000, 500)

	candidates := DetectTrafficAnomaly(stats, now, types.AppID("app-1"))
	if len(candidates) != 0 {
		t.Fatalf("expected zero candidates below the 14-day history floor, got %d", len(candidates))
	}
}

// TestDetectTrafficAnomaly_SpikyFixtureProducesOpportunity is the
// detector-quality "spiky/anomalous" fixture (Part K): a flat 28-day
// baseline followed by a sustained, sharp spike in the trailing
// evaluation window.
func TestDetectTrafficAnomaly_SpikyFixtureProducesOpportunity(t *testing.T) {
	now := time.Date(2026, 3, 15, 6, 0, 0, 0, time.UTC)
	stats := hourlyFixture(now, 30, 100, 1000, 500)

	// Spike the last 6 hours to 20x request volume (and proportional
	// cost/tokens) -- a sustained, unmistakable deviation.
	hourEnd := now.UTC().Truncate(time.Hour)
	for i := range stats {
		if !stats[i].Bucket.Before(hourEnd.Add(-6 * time.Hour)) {
			stats[i].Requests = 2000
			stats[i].TotalTokens = 2000 * 500
			stats[i].CostMicro = 2000 * 1000
		}
	}

	candidates := DetectTrafficAnomaly(stats, now, types.AppID("app-1"))
	if len(candidates) == 0 {
		t.Fatal("expected at least one candidate for a sustained traffic spike")
	}
	c := candidates[0]
	if c.Kind != KindTrafficAnomaly {
		t.Errorf("expected kind traffic_anomaly, got %q", c.Kind)
	}
	if c.Severity == nil {
		t.Fatal("expected severity to be set")
	}
	if c.SavingsMicro != 0 {
		t.Errorf("expected no savings figure on an anomaly, got %d", c.SavingsMicro)
	}
	if !CandidateMeetsSavingsFloor(c) {
		t.Error("expected traffic-anomaly candidates to be exempt from the savings floor")
	}
	ev, ok := c.Evidence.(TrafficAnomalyEvidence)
	if !ok {
		t.Fatalf("expected TrafficAnomalyEvidence, got %T", c.Evidence)
	}
	if ev.PeakZScore <= anomalyZThreshold {
		t.Errorf("expected peak z-score above threshold, got %.2f", ev.PeakZScore)
	}
	rec, ok := c.Recommendation.(TrafficAnomalyRecommendation)
	if !ok || rec.Action != "investigate" {
		t.Fatalf("expected an investigate recommendation, got %+v", c.Recommendation)
	}
}

// TestDetectTrafficAnomaly_ErrorSpikeIsHighSeverity checks Part G.3.4's
// severity rule: an error-rate spike over 10 percentage points is high
// severity even without an extreme cost z-score.
func TestDetectTrafficAnomaly_ErrorSpikeIsHighSeverity(t *testing.T) {
	now := time.Date(2026, 3, 15, 6, 0, 0, 0, time.UTC)
	stats := hourlyFixture(now, 30, 100, 1000, 500)

	hourEnd := now.UTC().Truncate(time.Hour)
	for i := range stats {
		if !stats[i].Bucket.Before(hourEnd.Add(-6 * time.Hour)) {
			// Keep request volume steady but drive an error-rate spike far
			// past 10pp -- requests unaffected, only reliability degrades.
			stats[i].Errors = 60
			stats[i].Requests = 100
		}
	}

	candidates := DetectTrafficAnomaly(stats, now, types.AppID("app-1"))
	if len(candidates) == 0 {
		t.Fatal("expected a candidate for a sustained error-rate spike")
	}
	c := candidates[0]
	if c.Severity == nil || *c.Severity != "high" {
		t.Errorf("expected high severity for a >10pp error-rate spike, got %+v", c.Severity)
	}
}
