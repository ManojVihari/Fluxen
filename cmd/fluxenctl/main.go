// Command fluxenctl is the admin CLI (Part C.1). Phase 0 implements only
// the migration subcommands, since Phase 0's database has nothing else to
// administer yet — org/user creation, key issuance, and demo-traffic
// seeding are added by the phases that introduce those entities.
package main

import (
	"fmt"
	"os"

	"fluxen/internal/config"
	"fluxen/internal/store"
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

Requires DATABASE_URL to be set in the environment.`)
}
