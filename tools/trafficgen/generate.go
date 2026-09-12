package trafficgen

import (
	"crypto/sha256"
	"fmt"
	"math/rand"
	"time"

	"fluxen/pkg/pricing"
	"fluxen/pkg/types"
)

// The two models every seeded request uses. Both must exist in the
// pricing catalog (pkg/pricing/catalog.yaml) — GenerateRecords prices
// every record through the real Calculate() function (Rule 9: never
// duplicate pricing logic), so an unpriced model here would silently
// produce cost_status=unknown traffic instead of the realistic cost data
// the demo scenario expects.
const (
	premiumModel = "gpt-4o"
	cheapModel   = "gpt-4o-mini"
)

// eligibleFraction is the share of premiumModel requests that are
// deliberately shaped like they didn't need the premium model — small
// token counts, no tools, no images — matching the PRD's own worked
// example (~61%) and the eligibility predicate Phase 3's Model Cost
// detector will apply.
const eligibleFraction = 0.65

// premiumShare is the overall share of requests served by premiumModel at
// all (eligible + genuinely-large-context); the remainder go straight to
// cheapModel as an efficient baseline.
const premiumShare = 0.75

// errorRate is the share of requests that fail, split across a couple of
// realistic error statuses, so Application Detail's error reporting has
// something real to show.
const errorRate = 0.02

// requestsPerDayMin/Max bound how many requests are generated for each
// day, randomized so the timeseries isn't a flat line. Sized (Part L
// Phase 3 backend tasks: "a fresh install must not need to wait days to
// see the Aha Moment") so the trailing-14-day window a fresh seed
// produces clears the Model Cost detector's suppression floors (Part
// G.2) with comfortable margin against random-seed variance, not just on
// average.
const requestsPerDayMin = 150
const requestsPerDayMax = 300

// GenerateRecords builds the full set of synthetic UsageRecords for
// opts.Days trailing days, ending on opts.Now. It is a pure function —
// no I/O, no wall-clock reads beyond opts.Now, deterministic for a given
// opts.Rand seed — so it's unit-testable without a database (Rule 8).
func GenerateRecords(opts Options, orgID types.OrgID, appID types.AppID, catalog *pricing.Catalog) []types.UsageRecord {
	rng := opts.Rand
	if rng == nil {
		rng = rand.New(rand.NewSource(1))
	}

	today := time.Date(opts.Now.Year(), opts.Now.Month(), opts.Now.Day(), 0, 0, 0, 0, time.UTC)
	var records []types.UsageRecord

	for dayOffset := opts.Days; dayOffset >= 0; dayOffset-- {
		day := today.AddDate(0, 0, -dayOffset)
		n := requestsPerDayMin + rng.Intn(requestsPerDayMax-requestsPerDayMin+1)

		for i := 0; i < n; i++ {
			records = append(records, generateOne(rng, orgID, appID, catalog, day))
		}
	}

	return records
}

func generateOne(rng *rand.Rand, orgID types.OrgID, appID types.AppID, catalog *pricing.Catalog, day time.Time) types.UsageRecord {
	startedAt := day.Add(time.Duration(rng.Int63n(int64(24 * time.Hour))))

	model, eligible := pickModel(rng)

	var inputTokens, outputTokens int
	var hasTools, hasImages bool
	if eligible {
		inputTokens = 300 + rng.Intn(2700) // 300..3000
		outputTokens = 50 + rng.Intn(350)  // 50..400
	} else {
		inputTokens = 20000 + rng.Intn(40000) // 20000..60000 — a genuinely large document
		outputTokens = 500 + rng.Intn(1000)   // 500..1500
		hasTools = rng.Float64() < 0.3
		hasImages = rng.Float64() < 0.1
	}

	usage := types.ResponseUsage{
		InputTokens:  inputTokens,
		OutputTokens: outputTokens,
		TotalTokens:  inputTokens + outputTokens,
	}
	costInput, costOutput, costTotal, costStatus := pricing.Calculate(catalog, model, usage)

	status := "ok"
	httpStatus := 200
	errorCode := ""
	if rng.Float64() < errorRate {
		if rng.Float64() < 0.5 {
			status, httpStatus, errorCode = "provider_error", 500, "provider_error"
		} else {
			status, httpStatus, errorCode = "timeout", 504, "upstream_timeout"
		}
	}

	durationMS := 400 + rng.Intn(1600)
	if !eligible {
		durationMS += 800 // larger context takes longer
	}

	streamed := rng.Float64() < 0.4
	messageCount := 1 + rng.Intn(4)

	var systemHash []byte
	if rng.Float64() < 0.8 {
		sum := sha256.Sum256([]byte(fmt.Sprintf("seeded-system-prompt-%d", rng.Intn(3))))
		systemHash = sum[:]
	}

	id := randomUUID(rng)

	return types.UsageRecord{
		ID: id, OrgID: orgID, AppID: appID, StartedAt: startedAt, DurationMS: durationMS,
		Endpoint: "chat.completions", Protocol: "openai", Streamed: streamed,
		RequestedModel: model, Provider: "openai", Model: model, RouteReason: "direct",
		InputTokens: usage.InputTokens, OutputTokens: usage.OutputTokens, TotalTokens: usage.TotalTokens,
		UsageSource: "provider",
		Cost:        costTotal, CostInput: costInput, CostOutput: costOutput,
		CostStatus: costStatus, PricingVersion: catalog.Version,
		CacheStatus: "disabled",
		Status:      status, HTTPStatus: httpStatus, ErrorCode: errorCode,
		WorkloadFeatures: types.WorkloadFeatures{
			HasTools: hasTools, HasImages: hasImages, MessageCount: messageCount, SystemPromptHash: systemHash,
		},
	}
}

// pickModel returns the model for one request and whether its token
// profile is the "small, cheap-model-eligible" shape — the deliberately
// injected inefficiency is premiumModel being used for a majority of
// these eligible-shaped requests instead of cheapModel.
func pickModel(rng *rand.Rand) (model string, eligibleShape bool) {
	if rng.Float64() >= premiumShare {
		return cheapModel, true // already on the cheap model — efficient baseline
	}
	// On the premium model: eligibleFraction of the time the request
	// didn't need to be — that's the inefficiency Phase 3's detector
	// exists to find. The remainder are genuinely large-context requests
	// that justify the premium model.
	return premiumModel, rng.Float64() < eligibleFraction
}

func randomUUID(rng *rand.Rand) string {
	var b [16]byte
	rng.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
