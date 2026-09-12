package measure

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"fluxen/internal/policy"
	"fluxen/internal/store"
	corepolicy "fluxen/pkg/policy"
	"fluxen/pkg/types"
)

// newTestPool starts a disposable, fully-migrated Postgres container —
// same self-contained pattern every other package's own test suite uses.
func newTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping testcontainers-based measure test in -short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	t.Cleanup(cancel)

	pgContainer, err := tcpostgres.Run(ctx, "postgres:16-alpine",
		tcpostgres.WithDatabase("fluxen"),
		tcpostgres.WithUsername("fluxen"),
		tcpostgres.WithPassword("fluxen"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").WithOccurrence(2).WithStartupTimeout(60*time.Second),
		),
	)
	if err != nil {
		t.Skipf("skipping: could not start postgres testcontainer (is Docker running?): %v", err)
	}
	t.Cleanup(func() { _ = pgContainer.Terminate(context.Background()) })

	dsn, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("failed to get connection string: %v", err)
	}

	sqlDB, err := store.OpenSQLDB(dsn)
	if err != nil {
		t.Fatalf("failed to open sql.DB: %v", err)
	}
	if err := store.MigrateUp(sqlDB); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	sqlDB.Close()

	pool, err := store.OpenPool(ctx, dsn)
	if err != nil {
		t.Fatalf("failed to open pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func seedApp(t *testing.T, pool *pgxpool.Pool) (types.OrgID, types.AppID) {
	t.Helper()
	orgs := store.NewOrganizations(pool)
	apps := store.NewApplications(pool)

	org, err := orgs.Create(context.Background(), "Acme")
	if err != nil {
		t.Fatalf("failed to seed organization: %v", err)
	}
	app, err := apps.Create(context.Background(), org.ID, "app", "App")
	if err != nil {
		t.Fatalf("failed to seed application: %v", err)
	}
	return org.ID, app.ID
}

// seedFlatTraffic inserts n requests for appID, evenly spread across
// [start, end), each costing costMicro — a flat-rate fixture simple
// enough to hand-verify a cost-per-1k figure against.
func seedFlatTraffic(t *testing.T, pool *pgxpool.Pool, orgID types.OrgID, appID types.AppID, start, end time.Time, n int, costMicro int64, idPrefix string) {
	t.Helper()
	reqs := store.NewRequests(pool)
	span := end.Sub(start)
	var records []types.UsageRecord
	for i := 0; i < n; i++ {
		offset := time.Duration(int64(span) * int64(i) / int64(n))
		records = append(records, types.UsageRecord{
			ID: fmt.Sprintf("00000000-0000-0000-%04s-%012d", idPrefix, i), OrgID: orgID, AppID: appID,
			StartedAt: start.Add(offset), DurationMS: 100,
			Endpoint: "chat.completions", Protocol: "openai", Streamed: false,
			RequestedModel: "gpt-4o", Provider: "openai", Model: "gpt-4o", RouteReason: "direct",
			InputTokens: 100, OutputTokens: 50, TotalTokens: 150, UsageSource: "provider",
			Cost: types.Money(costMicro), CostStatus: types.CostKnown, PricingVersion: "test",
			CacheStatus: "disabled", Status: "ok", HTTPStatus: 200,
		})
	}
	if err := reqs.InsertBatch(context.Background(), records); err != nil {
		t.Fatalf("failed to seed traffic: %v", err)
	}
}

func newRunner(pool *pgxpool.Pool, now time.Time) (*Runner, *store.Measurements, *store.Opportunities, *policy.Store, *store.Requests) {
	measurements := store.NewMeasurements(pool)
	opportunities := store.NewOpportunities(pool)
	policyStore := policy.NewStore(pool)
	requests := store.NewRequests(pool)

	r := NewRunner(measurements, opportunities, requests, policyStore, nil)
	r.Now = func() time.Time { return now }
	return r, measurements, opportunities, policyStore, requests
}

func seedOpportunity(t *testing.T, opportunities *store.Opportunities, orgID types.OrgID, appID types.AppID, expectedPct float64) store.Opportunity {
	t.Helper()
	opp, err := opportunities.UpsertOpen(context.Background(), store.Opportunity{
		OrgID: orgID, AppID: appID, Kind: "model_cost", Fingerprint: "fp-1",
		Title: "t", Summary: "s", WindowStart: time.Now().Add(-14 * 24 * time.Hour), WindowEnd: time.Now(),
		SampleRequests: 1000, CurrentCostMicro: 100, ProjectedCostMicro: 50, SavingsMicro: 50, SavingsPct: expectedPct,
		Confidence: "medium", ConfidenceScore: 0.6,
		Evidence: json.RawMessage(`{}`), Recommendation: json.RawMessage(`{}`), DetectorVersion: "v1",
	})
	if err != nil {
		t.Fatalf("failed to seed opportunity: %v", err)
	}
	return opp
}

// TestFullLifecycle_ApplyInterimFinal_Successful applies a policy change
// against seeded baseline traffic, seeds cheaper post-apply traffic, and
// fast-forwards through both checks to a successful verdict — Part L
// Phase 6's own demo scenario, minus the dashboard.
func TestFullLifecycle_ApplyInterimFinal_Successful(t *testing.T) {
	pool := newTestPool(t)
	orgID, appID := seedApp(t, pool)
	opportunities := store.NewOpportunities(pool)
	simulations := store.NewSimulations(pool)
	policyStore := policy.NewStore(pool)
	requests := store.NewRequests(pool)
	measurements := store.NewMeasurements(pool)

	appliedAt := time.Now().UTC()

	// Baseline: 1,000 requests at 10,000,000 micro each over the 14 days
	// before apply => cost_per_1k = 10,000,000,000.
	seedFlatTraffic(t, pool, orgID, appID, appliedAt.AddDate(0, 0, -14).Add(time.Hour), appliedAt.Add(-time.Hour), 1000, 10_000_000, "1")

	opp := seedOpportunity(t, opportunities, orgID, appID, 0.30) // expects a 30% reduction
	oppID := opp.ID
	sim, err := simulations.Create(context.Background(), store.Simulation{
		OrgID: orgID, AppID: appID, OpportunityID: &oppID,
		Scenario: json.RawMessage(`{"type":"model_mix"}`), WindowStart: appliedAt.AddDate(0, 0, -14), WindowEnd: appliedAt,
		Breakdown: json.RawMessage(`[]`), Assumptions: json.RawMessage(`[]`), EngineVersion: "v1",
	})
	if err != nil {
		t.Fatalf("failed to seed simulation: %v", err)
	}

	applier := &policy.Applier{
		Policies: policyStore, Opportunities: opportunities, Simulations: simulations,
		Measurer: NewFreezer(requests, measurements),
	}
	// Apply "now" — FreezeInput.AppliedAt is stamped inside Apply as
	// time.Now().UTC(), so re-derive the actual baseline window from
	// what gets created rather than assuming it exactly equals appliedAt.
	if _, err := applier.Apply(context.Background(), policy.ApplyInput{
		OrgID: orgID, AppID: appID, OpportunityID: opp.ID, SimulationID: sim.ID,
		Routing: policy.RoutingPatch{FromModel: "gpt-4o", ToModel: "gpt-4o-mini", Weight: 0.5, Sticky: true},
	}); err != nil {
		t.Fatalf("failed to apply: %v", err)
	}

	measurement, err := measurements.GetByOpportunity(context.Background(), orgID, opp.ID)
	if err != nil {
		t.Fatalf("failed to load the created measurement: %v", err)
	}
	if measurement.BaselineRequests != 1000 {
		t.Fatalf("expected baseline_requests=1000, got %d", measurement.BaselineRequests)
	}
	if measurement.BaselineCostPer1kMicro != 10_000_000_000 {
		t.Fatalf("expected baseline_cost_per_1k_micro=10,000,000,000, got %d", measurement.BaselineCostPer1kMicro)
	}

	realAppliedAt := measurement.AppliedAt

	// Post-apply traffic: 500 requests at 3,000,000 micro each over the
	// interim window => cost_per_1k = 3,000,000,000 — a 70% reduction,
	// comfortably clearing the 0.7*expected_pct=0.21 successful bar.
	seedFlatTraffic(t, pool, orgID, appID, realAppliedAt, realAppliedAt.AddDate(0, 0, 7), 500, 3_000_000, "2")

	interimRunner, _, _, _, _ := newRunner(pool, realAppliedAt.AddDate(0, 0, 7))
	if err := interimRunner.Run(context.Background()); err != nil {
		t.Fatalf("interim run failed: %v", err)
	}

	afterInterim, err := measurements.Get(context.Background(), orgID, measurement.ID)
	if err != nil {
		t.Fatalf("failed to reload measurement: %v", err)
	}
	if afterInterim.Status != "interim" {
		t.Fatalf("expected status=interim after the +7d check, got %q", afterInterim.Status)
	}
	if afterInterim.Verdict == nil || *afterInterim.Verdict != string(VerdictSuccessful) {
		t.Fatalf("expected verdict=successful, got %v", afterInterim.Verdict)
	}
	if *afterInterim.ObservedRequests != 500 {
		t.Errorf("expected observed_requests=500, got %d", *afterInterim.ObservedRequests)
	}

	// Extend the same cheap traffic through the full 14-day window for
	// the final check.
	seedFlatTraffic(t, pool, orgID, appID, realAppliedAt.AddDate(0, 0, 7), realAppliedAt.AddDate(0, 0, 14), 500, 3_000_000, "3")

	finalRunner, _, _, _, _ := newRunner(pool, realAppliedAt.AddDate(0, 0, 14))
	if err := finalRunner.Run(context.Background()); err != nil {
		t.Fatalf("final run failed: %v", err)
	}

	final, err := measurements.Get(context.Background(), orgID, measurement.ID)
	if err != nil {
		t.Fatalf("failed to reload measurement: %v", err)
	}
	if final.Status != "final" {
		t.Fatalf("expected status=final after the +14d check, got %q", final.Status)
	}
	if final.FinalizedAt == nil {
		t.Error("expected finalized_at to be set")
	}
	if final.Verdict == nil || *final.Verdict != string(VerdictSuccessful) {
		t.Fatalf("expected a final verdict=successful, got %v", final.Verdict)
	}
	if *final.ObservedRequests != 1000 {
		t.Errorf("expected observed_requests=1000 over the full 14-day window, got %d", *final.ObservedRequests)
	}

	realized, err := measurements.RealizedSavings(context.Background(), orgID)
	if err != nil {
		t.Fatalf("failed to sum realized savings: %v", err)
	}
	if realized != *final.ActualSavingsMicro {
		t.Errorf("expected realized savings to equal this measurement's own actual_savings_micro, got %d != %d", realized, *final.ActualSavingsMicro)
	}
}

// TestFullLifecycle_RegressedVerdict_RevertsCleanly deliberately seeds a
// regression (post-apply traffic costs more, not less) and confirms
// Revert restores the exact pre-apply policy document.
func TestFullLifecycle_RegressedVerdict_RevertsCleanly(t *testing.T) {
	pool := newTestPool(t)
	orgID, appID := seedApp(t, pool)
	opportunities := store.NewOpportunities(pool)
	simulations := store.NewSimulations(pool)
	policyStore := policy.NewStore(pool)
	requests := store.NewRequests(pool)
	measurements := store.NewMeasurements(pool)

	appliedAt := time.Now().UTC()
	seedFlatTraffic(t, pool, orgID, appID, appliedAt.AddDate(0, 0, -14).Add(time.Hour), appliedAt.Add(-time.Hour), 1000, 10_000_000, "1")

	opp := seedOpportunity(t, opportunities, orgID, appID, 0.30)
	oppID := opp.ID
	sim, err := simulations.Create(context.Background(), store.Simulation{
		OrgID: orgID, AppID: appID, OpportunityID: &oppID,
		Scenario: json.RawMessage(`{"type":"model_mix"}`), WindowStart: appliedAt.AddDate(0, 0, -14), WindowEnd: appliedAt,
		Breakdown: json.RawMessage(`[]`), Assumptions: json.RawMessage(`[]`), EngineVersion: "v1",
	})
	if err != nil {
		t.Fatalf("failed to seed simulation: %v", err)
	}

	applier := &policy.Applier{
		Policies: policyStore, Opportunities: opportunities, Simulations: simulations,
		Measurer: NewFreezer(requests, measurements),
	}
	if _, err := applier.Apply(context.Background(), policy.ApplyInput{
		OrgID: orgID, AppID: appID, OpportunityID: opp.ID, SimulationID: sim.ID,
		Routing: policy.RoutingPatch{FromModel: "gpt-4o", ToModel: "gpt-4o-mini", Weight: 0.5, Sticky: true},
	}); err != nil {
		t.Fatalf("failed to apply: %v", err)
	}

	preRevertDoc, err := policyStore.Get(context.Background(), appID)
	if err != nil {
		t.Fatalf("failed to load applied policy: %v", err)
	}
	if preRevertDoc.Document.Routing == nil {
		t.Fatal("expected the applied policy to carry a routing rule before revert")
	}

	measurement, err := measurements.GetByOpportunity(context.Background(), orgID, opp.ID)
	if err != nil {
		t.Fatalf("failed to load the created measurement: %v", err)
	}
	realAppliedAt := measurement.AppliedAt

	// Post-apply traffic costs MORE than baseline — a real regression.
	seedFlatTraffic(t, pool, orgID, appID, realAppliedAt, realAppliedAt.AddDate(0, 0, 14), 1000, 15_000_000, "2")

	finalRunner, _, _, _, _ := newRunner(pool, realAppliedAt.AddDate(0, 0, 14))
	if err := finalRunner.Run(context.Background()); err != nil {
		t.Fatalf("final run failed: %v", err)
	}

	final, err := measurements.Get(context.Background(), orgID, measurement.ID)
	if err != nil {
		t.Fatalf("failed to reload measurement: %v", err)
	}
	if final.Verdict == nil || *final.Verdict != string(VerdictRegressed) {
		t.Fatalf("expected verdict=regressed, got %v", final.Verdict)
	}

	reverter := &policy.Reverter{Policies: policyStore, Opportunities: opportunities, Measurements: measurements}
	reverted, err := reverter.Revert(context.Background(), orgID, measurement.ID, nil, "reverting a regression")
	if err != nil {
		t.Fatalf("revert failed: %v", err)
	}
	if reverted.Document.Routing != nil {
		t.Errorf("expected the reverted document to have no routing rule (the pre-apply state), got %+v", reverted.Document.Routing)
	}

	revertedOpp, err := opportunities.Get(context.Background(), orgID, opp.ID)
	if err != nil {
		t.Fatalf("failed to reload opportunity: %v", err)
	}
	if revertedOpp.Status != "reverted" {
		t.Errorf("expected the opportunity to transition to reverted, got %q", revertedOpp.Status)
	}

	revertedMeasurement, err := measurements.Get(context.Background(), orgID, measurement.ID)
	if err != nil {
		t.Fatalf("failed to reload measurement: %v", err)
	}
	if revertedMeasurement.Status != "reverted" {
		t.Errorf("expected the measurement to transition to reverted, got %q", revertedMeasurement.Status)
	}
}

// TestRunner_ConfoundDetection_MakesVerdictInconclusive confirms a
// second policy change landing inside the measurement window is
// detected and produces an inconclusive verdict, per Part G.5.
func TestRunner_ConfoundDetection_MakesVerdictInconclusive(t *testing.T) {
	pool := newTestPool(t)
	orgID, appID := seedApp(t, pool)
	opportunities := store.NewOpportunities(pool)
	simulations := store.NewSimulations(pool)
	policyStore := policy.NewStore(pool)
	requests := store.NewRequests(pool)
	measurements := store.NewMeasurements(pool)

	appliedAt := time.Now().UTC()
	seedFlatTraffic(t, pool, orgID, appID, appliedAt.AddDate(0, 0, -14).Add(time.Hour), appliedAt.Add(-time.Hour), 1000, 10_000_000, "1")

	opp := seedOpportunity(t, opportunities, orgID, appID, 0.30)
	oppID := opp.ID
	sim, err := simulations.Create(context.Background(), store.Simulation{
		OrgID: orgID, AppID: appID, OpportunityID: &oppID,
		Scenario: json.RawMessage(`{"type":"model_mix"}`), WindowStart: appliedAt.AddDate(0, 0, -14), WindowEnd: appliedAt,
		Breakdown: json.RawMessage(`[]`), Assumptions: json.RawMessage(`[]`), EngineVersion: "v1",
	})
	if err != nil {
		t.Fatalf("failed to seed simulation: %v", err)
	}

	applier := &policy.Applier{
		Policies: policyStore, Opportunities: opportunities, Simulations: simulations,
		Measurer: NewFreezer(requests, measurements),
	}
	if _, err := applier.Apply(context.Background(), policy.ApplyInput{
		OrgID: orgID, AppID: appID, OpportunityID: opp.ID, SimulationID: sim.ID,
		Routing: policy.RoutingPatch{FromModel: "gpt-4o", ToModel: "gpt-4o-mini", Weight: 0.5, Sticky: true},
	}); err != nil {
		t.Fatalf("failed to apply: %v", err)
	}

	measurement, err := measurements.GetByOpportunity(context.Background(), orgID, opp.ID)
	if err != nil {
		t.Fatalf("failed to load measurement: %v", err)
	}
	realAppliedAt := measurement.AppliedAt
	seedFlatTraffic(t, pool, orgID, appID, realAppliedAt, realAppliedAt.AddDate(0, 0, 7), 500, 3_000_000, "2")

	// A second, unrelated policy change lands inside the measurement window.
	current, err := policyStore.Get(context.Background(), appID)
	if err != nil {
		t.Fatalf("failed to load current policy: %v", err)
	}
	doc := current.Document
	doc.RateLimit = &corepolicy.RateLimitPolicy{Enabled: true, RequestsPerMinute: 60}
	if _, err := policyStore.Save(context.Background(), appID, current.Version, doc, policy.HistoryEntry{ChangeSource: "user"}); err != nil {
		t.Fatalf("failed to seed a confounding policy change: %v", err)
	}

	interimRunner, _, _, _, _ := newRunner(pool, realAppliedAt.AddDate(0, 0, 7))
	if err := interimRunner.Run(context.Background()); err != nil {
		t.Fatalf("interim run failed: %v", err)
	}

	afterInterim, err := measurements.Get(context.Background(), orgID, measurement.ID)
	if err != nil {
		t.Fatalf("failed to reload measurement: %v", err)
	}
	if afterInterim.Verdict == nil || *afterInterim.Verdict != string(VerdictInconclusive) {
		t.Fatalf("expected verdict=inconclusive due to the confounding policy change, got %v", afterInterim.Verdict)
	}
}
