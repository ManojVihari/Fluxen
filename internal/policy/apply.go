package policy

import (
	"context"
	"errors"
	"fmt"

	"fluxen/internal/store"
	corepolicy "fluxen/pkg/policy"
	"fluxen/pkg/types"
)

// ErrSimulationRequired is returned when the referenced simulation
// doesn't exist, or wasn't run against this opportunity — Phase 5's
// dependency on Phase 4: "Apply is gated on a simulation existing for
// the opportunity being applied."
var ErrSimulationRequired = errors.New("policy: applying requires an existing simulation for this opportunity")

// RoutingPatch is the only policy field V1's apply endpoint sets — the
// Model Cost opportunity's own recommendation shape (Part G.3.1),
// possibly user-edited before confirming (Part G.1: "the recommended
// (or edited) policy change"). Every other control is edited directly
// through the Policies editor (Part I.4), not through Apply.
type RoutingPatch struct {
	FromModel string
	ToModel   string
	Weight    float64
	Sticky    bool
}

// ApplyInput is everything Apply needs beyond what it looks up itself.
type ApplyInput struct {
	OrgID         types.OrgID
	AppID         types.AppID
	OpportunityID string
	SimulationID  string
	Routing       RoutingPatch
	Note          string
	ChangedBy     *types.UserID
}

// Applier is the apply transaction (Part G.5, Part L Phase 5 backend
// tasks): validate, save the new policy version with its history entry,
// transition the opportunity to applied, and invalidate the gateway's
// cached snapshot — all four happen, in that order, or none of the
// policy-affecting ones do (the opportunity transition is a separate,
// non-transactional step; see MarkApplied's own doc comment for why).
type Applier struct {
	Policies      *Store
	Opportunities *store.Opportunities
	Simulations   *store.Simulations
	Snapshot      *Snapshot // nil-safe: a caller without a live gateway (e.g. a test) can omit it
}

// Apply runs the transaction. The caller (internal/api's handler) owns
// requiring confirm: true in the request body (Rule 19) — Apply itself
// has no "are you sure" concept, it only ever runs once actually called.
func (a *Applier) Apply(ctx context.Context, in ApplyInput) (Record, error) {
	opp, err := a.Opportunities.Get(ctx, in.OrgID, in.OpportunityID)
	if err != nil {
		return Record{}, fmt.Errorf("policy: failed to load opportunity: %w", err)
	}
	if opp.AppID != in.AppID {
		return Record{}, fmt.Errorf("policy: opportunity does not belong to app_id")
	}

	sim, err := a.Simulations.Get(ctx, in.OrgID, in.SimulationID)
	if err != nil {
		return Record{}, fmt.Errorf("policy: failed to load simulation: %w", err)
	}
	if sim.OpportunityID == nil || *sim.OpportunityID != in.OpportunityID {
		return Record{}, ErrSimulationRequired
	}

	current, err := a.Policies.Get(ctx, in.AppID)
	if err != nil {
		return Record{}, err
	}

	target := current.Document
	target.Routing = &corepolicy.RoutingPolicy{
		Enabled: true, FromModel: in.Routing.FromModel, ToModel: in.Routing.ToModel,
		Weight: in.Routing.Weight, Sticky: in.Routing.Sticky,
	}
	if err := target.Validate(); err != nil {
		return Record{}, fmt.Errorf("policy: invalid target document: %w", err)
	}

	diff, err := Diff(&current.Document, &target)
	if err != nil {
		return Record{}, err
	}

	oppID, simID := in.OpportunityID, in.SimulationID
	var notePtr *string
	if in.Note != "" {
		notePtr = &in.Note
	}

	saved, err := a.Policies.Save(ctx, in.AppID, current.Version, target, HistoryEntry{
		ChangeSource: "opportunity", OpportunityID: &oppID, SimulationID: &simID,
		Note: notePtr, ChangedBy: in.ChangedBy, Diff: diff,
	})
	if err != nil {
		return Record{}, err
	}

	if _, err := a.Opportunities.MarkApplied(ctx, in.OrgID, in.OpportunityID); err != nil {
		return Record{}, fmt.Errorf("policy: policy saved but failed to transition opportunity to applied: %w", err)
	}

	freezeBaseline(ctx, in.AppID, in.OpportunityID)

	if a.Snapshot != nil {
		a.Snapshot.Invalidate(ctx, in.AppID)
	}

	return saved, nil
}

// freezeBaseline is Phase 6's hook, called here on purpose one phase
// early (Part L Phase 5 tests: "the full apply transaction including
// baseline handling stub — full baseline freeze lands in Phase 6, but
// apply.go must already call into it"). It is intentionally a no-op:
// Phase 6 builds the measurements table and the real 14-day-baseline
// capture (Part G.5) this name promises; building that table now would
// be Phase 6 work done early (Rule 16).
func freezeBaseline(_ context.Context, _ types.AppID, _ string) {}
