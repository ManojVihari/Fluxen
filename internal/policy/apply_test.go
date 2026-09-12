package policy

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"fluxen/internal/store"
)

func TestApplier_Apply_EndToEnd(t *testing.T) {
	pool := newTestPool(t)
	orgID, appID := seedApp(t, pool)

	opps := store.NewOpportunities(pool)
	sims := store.NewSimulations(pool)
	policies := NewStore(pool)

	opp, err := opps.UpsertOpen(context.Background(), store.Opportunity{
		OrgID: orgID, AppID: appID, Kind: "model_cost", Fingerprint: "fp-1",
		Title: "t", Summary: "s", WindowStart: time.Now().Add(-14 * 24 * time.Hour), WindowEnd: time.Now(),
		SampleRequests: 1000, CurrentCostMicro: 100, ProjectedCostMicro: 50, SavingsMicro: 50, SavingsPct: 0.5,
		Confidence: "medium", ConfidenceScore: 0.6,
		Evidence: json.RawMessage(`{}`), Recommendation: json.RawMessage(`{}`), DetectorVersion: "v1",
	})
	if err != nil {
		t.Fatalf("unexpected error seeding opportunity: %v", err)
	}

	oppID := opp.ID
	sim, err := sims.Create(context.Background(), store.Simulation{
		OrgID: orgID, AppID: appID, OpportunityID: &oppID,
		Scenario:    json.RawMessage(`{"type":"model_mix"}`),
		WindowStart: opp.WindowStart, WindowEnd: opp.WindowEnd,
		Breakdown: json.RawMessage(`[]`), Assumptions: json.RawMessage(`[]`),
		EngineVersion: "v1",
	})
	if err != nil {
		t.Fatalf("unexpected error seeding simulation: %v", err)
	}

	applier := &Applier{Policies: policies, Opportunities: opps, Simulations: sims}
	rec, err := applier.Apply(context.Background(), ApplyInput{
		OrgID: orgID, AppID: appID, OpportunityID: opp.ID, SimulationID: sim.ID,
		Routing: RoutingPatch{FromModel: "gpt-4o", ToModel: "gpt-4o-mini", Weight: 0.5, Sticky: true},
		Note:    "applying the aha moment",
	})
	if err != nil {
		t.Fatalf("unexpected error applying: %v", err)
	}
	if rec.Version != 1 {
		t.Errorf("expected version 1, got %d", rec.Version)
	}
	if rec.Document.Routing == nil || rec.Document.Routing.ToModel != "gpt-4o-mini" || rec.Document.Routing.Weight != 0.5 {
		t.Errorf("expected the saved policy to carry the routing patch, got %+v", rec.Document)
	}

	updatedOpp, err := opps.Get(context.Background(), orgID, opp.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updatedOpp.Status != "applied" {
		t.Errorf("expected the opportunity to transition to 'applied', got %q", updatedOpp.Status)
	}

	history, err := policies.History(context.Background(), appID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(history) != 1 || history[0].ChangeSource != "opportunity" {
		t.Fatalf("expected one history entry with change_source=opportunity, got %+v", history)
	}
	if history[0].OpportunityID == nil || *history[0].OpportunityID != opp.ID {
		t.Errorf("expected the history entry to link the opportunity, got %+v", history[0].OpportunityID)
	}
	if history[0].SimulationID == nil || *history[0].SimulationID != sim.ID {
		t.Errorf("expected the history entry to link the simulation, got %+v", history[0].SimulationID)
	}
}

func TestApplier_Apply_RejectsWithoutMatchingSimulation(t *testing.T) {
	pool := newTestPool(t)
	orgID, appID := seedApp(t, pool)

	opps := store.NewOpportunities(pool)
	sims := store.NewSimulations(pool)
	policies := NewStore(pool)

	opp, err := opps.UpsertOpen(context.Background(), store.Opportunity{
		OrgID: orgID, AppID: appID, Kind: "model_cost", Fingerprint: "fp-1",
		Title: "t", Summary: "s", WindowStart: time.Now().Add(-14 * 24 * time.Hour), WindowEnd: time.Now(),
		SampleRequests: 1000, CurrentCostMicro: 100, ProjectedCostMicro: 50, SavingsMicro: 50, SavingsPct: 0.5,
		Confidence: "medium", ConfidenceScore: 0.6,
		Evidence: json.RawMessage(`{}`), Recommendation: json.RawMessage(`{}`), DetectorVersion: "v1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// A simulation that exists but was never linked to this opportunity.
	sim, err := sims.Create(context.Background(), store.Simulation{
		OrgID: orgID, AppID: appID,
		Scenario: json.RawMessage(`{"type":"model_mix"}`), WindowStart: time.Now(), WindowEnd: time.Now(),
		Breakdown: json.RawMessage(`[]`), Assumptions: json.RawMessage(`[]`), EngineVersion: "v1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	applier := &Applier{Policies: policies, Opportunities: opps, Simulations: sims}
	_, err = applier.Apply(context.Background(), ApplyInput{
		OrgID: orgID, AppID: appID, OpportunityID: opp.ID, SimulationID: sim.ID,
		Routing: RoutingPatch{FromModel: "gpt-4o", ToModel: "gpt-4o-mini", Weight: 0.5},
	})
	if err != ErrSimulationRequired {
		t.Fatalf("expected ErrSimulationRequired, got %v", err)
	}
}
