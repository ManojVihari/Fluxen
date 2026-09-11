// Package trafficgen generates realistic synthetic request history
// directly into Postgres — a demo/dev tool, not part of the production
// gateway path (Part L Phase 2 backend tasks: "tools/trafficgen: demo/
// realistic traffic generator producing multi-model, multi-day traffic
// with deliberately injected inefficiency").
//
// The "deliberately injected inefficiency" is a large share of requests
// served by an expensive model despite having a token profile (small
// input/output, no tools, no images) that a cheaper model could plausibly
// handle — exactly the shape Phase 3's Model Cost detector will look for.
// Phase 2 only needs this fixture to demonstrate real multi-model,
// multi-day Application Detail data; Phase 3 is expected to tune the
// volume/shape further once the detector's actual thresholds exist (Part
// L Phase 3: "extend the Phase 2 fixture").
package trafficgen

import (
	"context"
	"fmt"
	"math/rand"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"fluxen/internal/auth"
	"fluxen/internal/rollup"
	"fluxen/internal/store"
	"fluxen/pkg/pricing"
	"fluxen/pkg/types"
)

// Options configures a seed run. Now and Rand are injectable so
// GenerateRecords is deterministic and testable without wall-clock
// flakiness.
type Options struct {
	OrgName     string
	OwnerEmail  string
	OwnerPasswd string
	AppName     string // "Document AI" -> slug "document-ai"
	Days        int    // how many trailing days of traffic to generate
	Now         time.Time
	Rand        *rand.Rand
}

// DefaultOptions returns the options `fluxenctl seed --demo` uses.
func DefaultOptions() Options {
	return Options{
		OrgName:     "Acme",
		OwnerEmail:  "owner@example.com",
		OwnerPasswd: "supersecret123",
		AppName:     "Document AI",
		Days:        30,
		Now:         time.Now().UTC(),
		Rand:        rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

// Result summarizes what a seed run did.
type Result struct {
	OrgID             types.OrgID
	AppID             types.AppID
	AppSlug           string
	RequestsGenerated int
	AlreadySeeded     bool // the application already had traffic; nothing new was generated
}

const insertBatchSize = 500

// Seed ensures the demo organization/owner/application exist (reusing
// them if a previous run — or the setup wizard — already created them),
// generates synthetic traffic for the application if it doesn't already
// have any, and recomputes rollups for the generated window so
// Application Detail reflects it immediately rather than waiting for the
// next scheduled job tick.
func Seed(ctx context.Context, pool *pgxpool.Pool, opts Options) (Result, error) {
	orgs := store.NewOrganizations(pool)
	users := store.NewUsers(pool)
	apps := store.NewApplications(pool)
	keys := store.NewAPIKeys(pool)

	orgID, err := ensureOrg(ctx, orgs, users, opts)
	if err != nil {
		return Result{}, err
	}

	app, created, err := ensureApp(ctx, apps, orgID, opts.AppName)
	if err != nil {
		return Result{}, err
	}

	if !created {
		hasTraffic, err := appHasTraffic(ctx, pool, app.ID)
		if err != nil {
			return Result{}, err
		}
		if hasTraffic {
			return Result{OrgID: orgID, AppID: app.ID, AppSlug: app.Slug, AlreadySeeded: true}, nil
		}
	}

	if _, _, err := keys.Create(ctx, app.ID, "seed"); err != nil {
		return Result{}, fmt.Errorf("trafficgen: failed to issue a key for the seeded application: %w", err)
	}

	catalog, err := pricing.LoadEmbedded()
	if err != nil {
		return Result{}, fmt.Errorf("trafficgen: failed to load pricing catalog: %w", err)
	}

	records := GenerateRecords(opts, orgID, app.ID, catalog)

	reqs := store.NewRequests(pool)
	for i := 0; i < len(records); i += insertBatchSize {
		end := min(i+insertBatchSize, len(records))
		if err := reqs.InsertBatch(ctx, records[i:end]); err != nil {
			return Result{}, fmt.Errorf("trafficgen: failed to insert generated traffic: %w", err)
		}
	}

	if err := apps.MarkSeen(ctx, app.ID, opts.Now); err != nil {
		return Result{}, fmt.Errorf("trafficgen: failed to mark application seen: %w", err)
	}

	// Recompute rollups for the whole generated window immediately —
	// otherwise a fresh install would need to wait for the scheduled
	// rollup.hourly/rollup.daily jobs to catch up before Application
	// Detail shows anything (Part L Phase 2 demo scenario: seed then
	// "open Application Detail ... and see correct 30-day spend").
	windowStart := time.Date(opts.Now.Year(), opts.Now.Month(), opts.Now.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, -opts.Days-1)
	windowEnd := time.Date(opts.Now.Year(), opts.Now.Month(), opts.Now.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, 1)
	if err := rollup.ComputeHourly(ctx, pool, windowStart, windowEnd); err != nil {
		return Result{}, fmt.Errorf("trafficgen: failed to compute hourly rollups for seeded traffic: %w", err)
	}
	if err := rollup.ComputeDaily(ctx, pool, windowStart, windowEnd); err != nil {
		return Result{}, fmt.Errorf("trafficgen: failed to compute daily rollups for seeded traffic: %w", err)
	}

	return Result{OrgID: orgID, AppID: app.ID, AppSlug: app.Slug, RequestsGenerated: len(records)}, nil
}

func ensureOrg(ctx context.Context, orgs *store.Organizations, users *store.Users, opts Options) (types.OrgID, error) {
	if existing, found, err := orgs.First(ctx); err != nil {
		return "", err
	} else if found {
		return existing.ID, nil
	}

	org, err := orgs.Create(ctx, opts.OrgName)
	if err != nil {
		return "", fmt.Errorf("trafficgen: failed to create organization: %w", err)
	}

	hash, err := auth.HashPassword(opts.OwnerPasswd)
	if err != nil {
		return "", err
	}
	if _, err := users.Create(ctx, org.ID, opts.OwnerEmail, hash, "owner"); err != nil {
		return "", fmt.Errorf("trafficgen: failed to create owner user: %w", err)
	}

	return org.ID, nil
}

func ensureApp(ctx context.Context, apps *store.Applications, orgID types.OrgID, name string) (store.Application, bool, error) {
	slug := store.Slugify(name)

	existing, err := apps.GetBySlug(ctx, orgID, slug)
	if err == nil {
		return existing, false, nil
	}
	if err != store.ErrNotFound {
		return store.Application{}, false, err
	}

	created, err := apps.Create(ctx, orgID, slug, name)
	if err != nil {
		return store.Application{}, false, fmt.Errorf("trafficgen: failed to create application: %w", err)
	}
	return created, true, nil
}

func appHasTraffic(ctx context.Context, pool *pgxpool.Pool, appID types.AppID) (bool, error) {
	var exists bool
	err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM requests WHERE app_id = $1)`, appID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("trafficgen: failed to check for existing traffic: %w", err)
	}
	return exists, nil
}
