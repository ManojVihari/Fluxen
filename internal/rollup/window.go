// Package rollup turns raw requests rows into the hourly/daily aggregates
// Application Detail actually reads (Part C.7/E.1 of the implementation
// specification). Every compute function here is idempotent — it deletes
// then re-inserts its target bucket range — so re-running a job for the
// same window never double-counts, and a late-arriving request (ingest
// lag) is picked up correctly the next time that window is recomputed.
package rollup

import "time"

// hourlyLookback controls how far behind "now" HourlyWindow reaches back:
// the current (partial) hour plus two completed hours behind it, enough
// to absorb the ingest writer's flush lag (Part B.2) without recomputing
// the whole table on every 5-minute tick.
const hourlyLookbackHours = 2

// HourlyWindow returns the [from, to) bucket range a rollup.hourly run
// starting at now should recompute, both UTC-aligned to the hour.
func HourlyWindow(now time.Time) (from, to time.Time) {
	now = now.UTC()
	currentHour := now.Truncate(time.Hour)
	return currentHour.Add(-hourlyLookbackHours * time.Hour), currentHour.Add(time.Hour)
}

// dailyLookbackDays covers yesterday and today — yesterday in case a
// request landed just before midnight UTC but wasn't rolled up until
// after, today so Application Detail reflects the current day without
// waiting for it to end.
const dailyLookbackDays = 1

// DailyWindow returns the [from, to) day range a rollup.daily run starting
// at now should recompute, both UTC-aligned to midnight.
func DailyWindow(now time.Time) (from, to time.Time) {
	now = now.UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	return today.AddDate(0, 0, -dailyLookbackDays), today.AddDate(0, 0, 1)
}
