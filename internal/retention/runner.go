// Package retention runs Part E.2's retention rules daily
// (retention.enforce, Part C.7's job table) — dropping expired raw
// requests, clearing expired captured bodies independently of the
// requests-row TTL, and dropping old dismissed/stale opportunities.
package retention

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"fluxen/internal/store"
)

// Runner is the only piece of this package that touches Postgres.
type Runner struct {
	Orgs      *store.Organizations
	Retention *store.Retention
	Logger    *slog.Logger

	// Now is injectable for deterministic tests; nil means time.Now.
	Now func() time.Time
}

func NewRunner(orgs *store.Organizations, retention *store.Retention, logger *slog.Logger) *Runner {
	if logger == nil {
		logger = slog.Default()
	}
	return &Runner{Orgs: orgs, Retention: retention, Logger: logger}
}

func (r *Runner) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

// Run enforces retention for every organization in the deployment. One
// org's failure is logged and skipped, never fatal to the rest (Part
// C.7's job contract).
func (r *Runner) Run(ctx context.Context) error {
	orgIDs, err := r.Orgs.AllIDs(ctx)
	if err != nil {
		return fmt.Errorf("retention: failed to list organizations: %w", err)
	}

	now := r.now().UTC()
	for _, orgID := range orgIDs {
		settings, err := r.Orgs.GetRetentionSettings(ctx, orgID)
		if err != nil {
			r.Logger.Error("retention: failed to load settings", "org_id", orgID, "error", err)
			continue
		}

		droppedRequests, clearedBodies, err := r.Retention.EnforceRequests(ctx, orgID, settings, now)
		if err != nil {
			r.Logger.Error("retention: failed to enforce request retention", "org_id", orgID, "error", err)
		} else if droppedRequests > 0 || clearedBodies > 0 {
			r.Logger.Info("retention: enforced request retention", "org_id", orgID, "dropped_requests", droppedRequests, "cleared_bodies", clearedBodies)
		}

		droppedOpps, err := r.Retention.EnforceOpportunities(ctx, orgID, now)
		if err != nil {
			r.Logger.Error("retention: failed to enforce opportunity retention", "org_id", orgID, "error", err)
		} else if droppedOpps > 0 {
			r.Logger.Info("retention: enforced opportunity retention", "org_id", orgID, "dropped_opportunities", droppedOpps)
		}
	}
	return nil
}
