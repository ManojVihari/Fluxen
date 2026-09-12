package measure

import (
	"context"
	"fmt"

	"fluxen/internal/policy"
	"fluxen/internal/store"
)

// Freezer implements policy.BaselineFreezer — the concrete Phase 6 side
// of the interface internal/policy declares, kept in this package (not
// policy's) so internal/policy never has to import internal/measure
// (which itself imports internal/policy for confound detection).
type Freezer struct {
	Requests     *store.Requests
	Measurements *store.Measurements
}

func NewFreezer(requests *store.Requests, measurements *store.Measurements) *Freezer {
	return &Freezer{Requests: requests, Measurements: measurements}
}

// Freeze computes the pre-apply baseline from real requests and creates
// the measurement row that Runner will later fill in via interim/final
// checks (Part G.5 step 4).
func (f *Freezer) Freeze(ctx context.Context, in policy.FreezeInput) error {
	start, end, reqCount, costMicro, costPer1k, err := FreezeBaseline(ctx, f.Requests, in.AppID, in.AppliedAt)
	if err != nil {
		return fmt.Errorf("measure: failed to compute baseline: %w", err)
	}

	var simID *string
	if in.SimulationID != "" {
		simID = &in.SimulationID
	}

	_, err = f.Measurements.Create(ctx, store.Measurement{
		OrgID: in.OrgID, AppID: in.AppID, OpportunityID: in.OpportunityID, SimulationID: simID,
		PolicyVersion: in.PolicyVersion, AppliedAt: in.AppliedAt,
		BaselineStart: start, BaselineEnd: end, BaselineRequests: reqCount, BaselineCostMicro: costMicro, BaselineCostPer1kMicro: costPer1k,
		ExpectedSavingsMicro: in.ExpectedSavingsMicro, ExpectedPct: in.ExpectedPct,
	})
	if err != nil {
		return fmt.Errorf("measure: failed to create measurement: %w", err)
	}
	return nil
}
