// Package health implements the readiness checks Fluxen's datastores
// expose to /readyz. It knows nothing about HTTP; internal/health is a pure
// dependency-status package so it can be unit tested and reused by both the
// combined fluxen binary and the split gateway/control binaries.
package health

import (
	"context"
)

// Pinger is satisfied by any dependency Fluxen must confirm is reachable
// before declaring itself ready (Postgres via pgxpool.Pool, Redis via
// redis.Client). Keeping this as a one-method interface, rather than
// depending on the concrete client types, keeps internal/health free of
// driver-specific imports and easy to test with a fake.
type Pinger interface {
	Ping(ctx context.Context) error
}

// Status is the outcome of checking one dependency.
type Status struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// Check pings every named dependency and reports its status. It never
// returns an error itself — a dependency being down is a normal, reportable
// outcome, not a Check failure.
func Check(ctx context.Context, deps map[string]Pinger) map[string]Status {
	result := make(map[string]Status, len(deps))
	for name, dep := range deps {
		if err := dep.Ping(ctx); err != nil {
			result[name] = Status{OK: false, Error: err.Error()}
			continue
		}
		result[name] = Status{OK: true}
	}
	return result
}

// AllOK reports whether every dependency in the result set is healthy.
func AllOK(result map[string]Status) bool {
	for _, s := range result {
		if !s.OK {
			return false
		}
	}
	return true
}
