// Command fluxenctl is the admin CLI (Part C.1): migration management and
// demo-data seeding.
package main

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"time"

	"fluxen/internal/config"
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

Requires DATABASE_URL to be set in the environment.`)
}
