package measure

import "testing"

func TestDetermineVerdict_Successful(t *testing.T) {
	v, _ := DetermineVerdict(0.30, 0.25, 1000, 1000, false) // 0.25 >= 0.7*0.30 = 0.21
	if v != VerdictSuccessful {
		t.Errorf("expected successful, got %v", v)
	}
}

func TestDetermineVerdict_Partial(t *testing.T) {
	v, _ := DetermineVerdict(0.30, 0.10, 1000, 1000, false) // 0.06 <= 0.10 < 0.21
	if v != VerdictPartial {
		t.Errorf("expected partial, got %v", v)
	}
}

func TestDetermineVerdict_NoEffect(t *testing.T) {
	v, _ := DetermineVerdict(0.30, 0.01, 1000, 1000, false)
	if v != VerdictNoEffect {
		t.Errorf("expected no_effect, got %v", v)
	}
}

func TestDetermineVerdict_NoEffect_SmallNegative(t *testing.T) {
	v, _ := DetermineVerdict(0.30, -0.01, 1000, 1000, false)
	if v != VerdictNoEffect {
		t.Errorf("expected no_effect for a small negative pct, got %v", v)
	}
}

func TestDetermineVerdict_Regressed(t *testing.T) {
	v, _ := DetermineVerdict(0.30, -0.05, 1000, 1000, false)
	if v != VerdictRegressed {
		t.Errorf("expected regressed, got %v", v)
	}
}

func TestDetermineVerdict_Regressed_NeverReclassifiedAsSuccessfulForTinyExpectedPct(t *testing.T) {
	// A tiny expected_pct would trivially satisfy actual_pct >= 0.7*expected_pct
	// even for a real regression, if regressed weren't checked first.
	v, _ := DetermineVerdict(0.01, -0.05, 1000, 1000, false)
	if v != VerdictRegressed {
		t.Errorf("expected regressed to take priority over successful, got %v", v)
	}
}

func TestDetermineVerdict_Inconclusive_Confound(t *testing.T) {
	v, reason := DetermineVerdict(0.30, 0.25, 1000, 1000, true)
	if v != VerdictInconclusive {
		t.Errorf("expected inconclusive when a confound is present, got %v", v)
	}
	if reason == "" {
		t.Error("expected a non-empty reason")
	}
}

func TestDetermineVerdict_Inconclusive_LowVolume(t *testing.T) {
	// observed=200 is 20% of baseline=1000, below the 30% floor.
	v, _ := DetermineVerdict(0.30, 0.25, 200, 1000, false)
	if v != VerdictInconclusive {
		t.Errorf("expected inconclusive for low observed volume, got %v", v)
	}
}

func TestDetermineVerdict_Inconclusive_LowVolumeExactlyAtFloorIsNotInconclusive(t *testing.T) {
	// observed=300 is exactly 30% of baseline=1000 — the floor itself
	// should not trigger inconclusive (only strictly below it).
	v, _ := DetermineVerdict(0.30, 0.25, 300, 1000, false)
	if v == VerdictInconclusive {
		t.Errorf("expected exactly-30%% observed volume to not be inconclusive, got %v", v)
	}
}

func TestDetermineVerdict_Inconclusive_PositiveButBelowPartialBand(t *testing.T) {
	// actual_pct=0.03 clears the no_effect band (>=0.02) but is below
	// 0.2*expected_pct=0.06 — doesn't fit partial or successful either.
	v, _ := DetermineVerdict(0.30, 0.03, 1000, 1000, false)
	if v != VerdictInconclusive {
		t.Errorf("expected inconclusive for a positive effect below the partial band, got %v", v)
	}
}

func TestDetermineVerdict_ZeroBaselineNeverBlocksLowVolumeCheck(t *testing.T) {
	// baselineRequests=0 must not divide by zero in the volume check.
	v, _ := DetermineVerdict(0.30, 0.25, 1000, 0, false)
	if v != VerdictSuccessful {
		t.Errorf("expected a zero baseline to not force inconclusive, got %v", v)
	}
}
