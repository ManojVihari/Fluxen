package api

import (
	"net/http"
	"time"
)

// parseRange reads the ?range= query parameter shared by the
// summary/timeseries/models endpoints and returns a whole-day [since,
// until) window in UTC. until is always the start of *tomorrow* (not the
// current instant) so that today's rollup — whose day column equals
// today's date regardless of what time it is right now — is included
// rather than excluded by a same-day "day < until::date" comparison.
//
// An unrecognized or missing range value falls back to the 30-day
// default rather than 400ing, since a bad range is a display preference,
// not a client error worth rejecting the request over.
func parseRange(r *http.Request) (since, until time.Time) {
	now := time.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	until = today.AddDate(0, 0, 1)

	var days int
	switch r.URL.Query().Get("range") {
	case "24h":
		days = 1
	case "7d":
		days = 7
	case "90d":
		days = 90
	case "30d", "":
		days = 30
	default:
		days = 30
	}

	since = until.AddDate(0, 0, -days)
	return since, until
}
