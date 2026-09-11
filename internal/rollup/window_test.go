package rollup

import (
	"testing"
	"time"
)

func TestHourlyWindow_AlignedAndCoversLookback(t *testing.T) {
	now := time.Date(2026, 3, 15, 14, 37, 22, 0, time.UTC)
	from, to := HourlyWindow(now)

	wantFrom := time.Date(2026, 3, 15, 12, 0, 0, 0, time.UTC)
	wantTo := time.Date(2026, 3, 15, 15, 0, 0, 0, time.UTC)

	if !from.Equal(wantFrom) {
		t.Errorf("expected from=%v, got %v", wantFrom, from)
	}
	if !to.Equal(wantTo) {
		t.Errorf("expected to=%v, got %v", wantTo, to)
	}
	if !now.After(from) || !now.Before(to) {
		t.Error("expected the window to contain now")
	}
}

func TestHourlyWindow_ConvertsToUTC(t *testing.T) {
	loc := time.FixedZone("UTC-5", -5*60*60)
	now := time.Date(2026, 3, 15, 9, 0, 0, 0, loc) // 14:00 UTC
	from, to := HourlyWindow(now)

	if from.Location() != time.UTC || to.Location() != time.UTC {
		t.Error("expected the window boundaries to be in UTC regardless of the input's location")
	}
	wantFrom := time.Date(2026, 3, 15, 12, 0, 0, 0, time.UTC)
	if !from.Equal(wantFrom) {
		t.Errorf("expected from=%v (computed from the UTC-equivalent hour), got %v", wantFrom, from)
	}
}

func TestDailyWindow_CoversYesterdayAndToday(t *testing.T) {
	now := time.Date(2026, 3, 15, 23, 59, 0, 0, time.UTC)
	from, to := DailyWindow(now)

	wantFrom := time.Date(2026, 3, 14, 0, 0, 0, 0, time.UTC)
	wantTo := time.Date(2026, 3, 16, 0, 0, 0, 0, time.UTC)

	if !from.Equal(wantFrom) {
		t.Errorf("expected from=%v, got %v", wantFrom, from)
	}
	if !to.Equal(wantTo) {
		t.Errorf("expected to=%v, got %v", wantTo, to)
	}
}
