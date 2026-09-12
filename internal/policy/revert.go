package policy

import (
	"context"
	"fmt"

	"fluxen/internal/store"
	corepolicy "fluxen/pkg/policy"
	"fluxen/pkg/types"
)

// Reverter is the one-click Revert a `regressed` verdict surfaces (Part
// G.5): restore the policy document exactly as it was immediately before
// the apply that's being reverted, as a brand-new version (never
// rewriting history), and mark both the opportunity and the measurement
// reverted.
type Reverter struct {
	Policies      *Store
	Opportunities *store.Opportunities
	Measurements  *store.Measurements
	Snapshot      *Snapshot
}

// Revert restores the policy document to what it was immediately before
// the given measurement's apply, as a new version, and transitions both
// the opportunity and the measurement to reverted.
func (rv *Reverter) Revert(ctx context.Context, orgID types.OrgID, measurementID string, changedBy *types.UserID, note string) (Record, error) {
	measurement, err := rv.Measurements.Get(ctx, orgID, measurementID)
	if err != nil {
		return Record{}, fmt.Errorf("policy: failed to load measurement: %w", err)
	}
	appID := measurement.AppID

	priorVersion := measurement.PolicyVersion - 1
	var priorDoc corepolicy.PolicyDocument
	if priorVersion > 0 {
		priorDoc, err = rv.Policies.AtVersion(ctx, appID, priorVersion)
		if err != nil {
			return Record{}, fmt.Errorf("policy: failed to load the pre-apply policy version: %w", err)
		}
	}
	// priorVersion == 0 means the application had no policy at all
	// before this apply — priorDoc stays the zero-value (empty, no-op)
	// document, exactly what an app with no policy behaves like.

	current, err := rv.Policies.Get(ctx, appID)
	if err != nil {
		return Record{}, err
	}

	diff, err := Diff(&current.Document, &priorDoc)
	if err != nil {
		return Record{}, err
	}

	var notePtr *string
	if note != "" {
		notePtr = &note
	}

	saved, err := rv.Policies.Save(ctx, appID, current.Version, priorDoc, HistoryEntry{
		ChangeSource: "revert", Note: notePtr, ChangedBy: changedBy, Diff: diff,
	})
	if err != nil {
		return Record{}, err
	}

	if _, err := rv.Opportunities.MarkReverted(ctx, orgID, measurement.OpportunityID); err != nil {
		return Record{}, fmt.Errorf("policy: policy reverted but failed to transition the opportunity to reverted: %w", err)
	}
	if _, err := rv.Measurements.MarkReverted(ctx, orgID, measurementID); err != nil {
		return Record{}, fmt.Errorf("policy: policy reverted but failed to mark the measurement reverted: %w", err)
	}

	if rv.Snapshot != nil {
		rv.Snapshot.Invalidate(ctx, appID)
	}

	return saved, nil
}
