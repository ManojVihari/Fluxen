// Command fluxenctl is the admin CLI (Part C.1): migration management and
// demo-data seeding.
package main

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"time"

	"fluxen/internal/config"
	"fluxen/internal/measure"
	"fluxen/internal/policy"
	"fluxen/internal/store"
	"fluxen/tools/trafficgen"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "fluxenctl:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) < 1 {
		return usageError()
	}

	switch args[0] {
	case "migrate":
		return runMigrate(args[1:])
	case "seed":
		return runSeed(args[1:])
	case "measure":
		return runMeasure(args[1:])
	case "-h", "--help", "help":
		printUsage()
		return nil
	default:
		return usageError()
	}
}

func runMigrate(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: fluxenctl migrate <up|down|status>")
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	db, err := store.OpenSQLDB(cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer db.Close()

	switch args[0] {
	case "up":
		return store.MigrateUp(db)
	case "down":
		return store.MigrateDown(db)
	case "status":
		return store.MigrateStatus(db)
	default:
		return fmt.Errorf("unknown migrate subcommand %q (want up|down|status)", args[0])
	}
}

// runSeed implements `fluxenctl seed --demo` (Part L Phase 2: the exact
// invocation the demo scenario and Phase 3's "fresh install" story both
// depend on). It reuses an existing organization/application if one
// already exists — safe to run more than once.
func runSeed(args []string) error {
	demo := false
	for _, a := range args {
		if a == "--demo" {
			demo = true
		}
	}
	if !demo {
		return fmt.Errorf("usage: fluxenctl seed --demo")
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	pool, err := store.OpenPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	opts := trafficgen.DefaultOptions()
	opts.Rand = rand.New(rand.NewSource(time.Now().UnixNano()))

	result, err := trafficgen.Seed(ctx, pool, opts)
	if err != nil {
		return err
	}

	if result.AlreadySeeded {
		fmt.Printf("fluxenctl: %q already has traffic — nothing to do.\n", result.AppSlug)
		return nil
	}

	fmt.Printf("fluxenctl: seeded %d requests for application %q (slug=%s).\n", result.RequestsGenerated, result.AppSlug, result.AppSlug)
	fmt.Println("fluxenctl: open the dashboard and sign in as owner@example.com / supersecret123 (if this was a fresh install).")
	return nil
}

// runMeasure implements `fluxenctl measure check [--fast-forward=<dur>]`
// (Part L Phase 6: "fluxenctl gets a clock-fast-forward affordance for
// testing without a real two-week wait"). Without --fast-forward it runs
// exactly what the scheduled measure.check job would at this instant;
// with it, every check is evaluated as if that much additional time had
// passed since each measurement's applied_at — enough to walk a freshly
// applied change through its interim and final verdicts in one command,
// for a demo or for testing.
func runMeasure(args []string) error {
	if len(args) < 1 || args[0] != "check" {
		return fmt.Errorf("usage: fluxenctl measure check [--fast-forward=<duration>]")
	}

	var fastForward time.Duration
	for _, a := range args[1:] {
		const prefix = "--fast-forward="
		if strings.HasPrefix(a, prefix) {
			d, err := parseFastForward(strings.TrimPrefix(a, prefix))
			if err != nil {
				return fmt.Errorf("invalid --fast-forward value: %w", err)
			}
			fastForward = d
		}
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	pool, err := store.OpenPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	runner := measure.NewRunner(store.NewMeasurements(pool), store.NewOpportunities(pool), store.NewRequests(pool), policy.NewStore(pool), nil)
	if fastForward > 0 {
		now := time.Now().UTC().Add(fastForward)
		runner.Now = func() time.Time { return now }
		fmt.Printf("fluxenctl: fast-forwarding %s (checking as of %s)\n", fastForward, now.Format(time.RFC3339))
	}

	if err := runner.Run(ctx); err != nil {
		return err
	}
	fmt.Println("fluxenctl: measurement check complete.")
	return nil
}

// parseFastForward accepts either a Go duration ("336h") or a bare
// integer number of days ("14d") — the latter is the natural unit for
// this command's own use case (Part G.5's +7d/+14d checkpoints) and
// time.ParseDuration alone doesn't support "d".
func parseFastForward(s string) (time.Duration, error) {
	if strings.HasSuffix(s, "d") {
		days, err := strconv.Atoi(strings.TrimSuffix(s, "d"))
		if err != nil {
			return 0, fmt.Errorf("expected an integer number of days before 'd', got %q", s)
		}
		return time.Duration(days) * 24 * time.Hour, nil
	}
	return time.ParseDuration(s)
}

func usageError() error {
	printUsage()
	return fmt.Errorf("invalid usage")
}

func printUsage() {
	fmt.Println(`fluxenctl — Fluxen admin CLI

Usage:
  fluxenctl migrate up       Apply all pending database migrations
  fluxenctl migrate down     Roll back the most recent migration
  fluxenctl migrate status   Show applied/pending migration status
  fluxenctl seed --demo      Seed a demo organization, application, and
                             30 days of realistic synthetic traffic
  fluxenctl measure check [--fast-forward=<duration>]
                             Run interim (+7d) / final (+14d) measurement
                             checks now. --fast-forward=14d (or any Go
                             duration, e.g. 336h) evaluates checks as if
                             that much time had passed since apply —
                             skip the real two-week wait for a demo.

Requires DATABASE_URL to be set in the environment.`)
}
