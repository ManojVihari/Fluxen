// Package db embeds the goose SQL migrations so they ship inside every
// Fluxen binary — no separate migrations directory needs to be mounted or
// copied into a deployment. internal/store wraps this embedded filesystem
// with the actual migration-running logic; this package's only job is to
// hold the embed directive next to the files it embeds (go:embed cannot
// reach outside its own package directory).
package db

import "embed"

//go:embed migrations/*.sql
var Migrations embed.FS
