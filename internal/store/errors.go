package store

import "errors"

// ErrNotFound is returned by a Get when no row matches the given scope
// (e.g. an application id that doesn't belong to the caller's org) — the
// API layer maps this to 404, never leaking whether the id exists for a
// different org.
var ErrNotFound = errors.New("store: not found")
