package detect

import "testing"

func TestAppMeetsFloors(t *testing.T) {
	cases := []struct {
		name          string
		requests14d   int64
		spend14dMicro int64
		want          bool
	}{
		{"clears both floors", 1500, 6_000_000, true},
		{"exactly at both floors", MinAppRequests14D, MinAppSpend14DMicro, true},
		{"too few requests", 999, 10_000_000, false},
		{"too little spend", 5000, 4_999_999, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := AppMeetsFloors(tc.requests14d, tc.spend14dMicro); got != tc.want {
				t.Errorf("AppMeetsFloors(%d, %d) = %v, want %v", tc.requests14d, tc.spend14dMicro, got, tc.want)
			}
		})
	}
}

func TestCandidateMeetsSavingsFloor(t *testing.T) {
	cases := []struct {
		name string
		c    Candidate
		want bool
	}{
		{"clears both", Candidate{SavingsMicro: 20_000_000, SavingsPct: 0.10}, true},
		{"savings too small", Candidate{SavingsMicro: 9_999_999, SavingsPct: 0.10}, false},
		{"pct too small", Candidate{SavingsMicro: 20_000_000, SavingsPct: 0.01}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := CandidateMeetsSavingsFloor(tc.c); got != tc.want {
				t.Errorf("CandidateMeetsSavingsFloor(%+v) = %v, want %v", tc.c, got, tc.want)
			}
		})
	}
}

func TestRankAndCap_OrdersBySavingsTimesConfidenceAndCapsAtFive(t *testing.T) {
	mk := func(name string, savings int64, conf Confidence) Candidate {
		return Candidate{Fingerprint: name, SavingsMicro: savings, Confidence: conf, ConfidenceScore: conf.Score()}
	}

	candidates := []Candidate{
		mk("low-big-savings", 100_000_000, ConfidenceLow),      // score 30,000,000
		mk("high-small-savings", 10_000_000, ConfidenceHigh),   // score 10,000,000
		mk("medium-mid-savings", 40_000_000, ConfidenceMedium), // score 24,000,000
		mk("d", 1, ConfidenceLow),
		mk("e", 1, ConfidenceLow),
		mk("f", 1, ConfidenceLow),
	}

	ranked := RankAndCap(candidates)

	if len(ranked) != MaxOpenOpportunitiesPerApp {
		t.Fatalf("expected the result capped at %d, got %d", MaxOpenOpportunitiesPerApp, len(ranked))
	}
	if ranked[0].Fingerprint != "low-big-savings" {
		t.Errorf("expected highest savings*confidence first, got %q", ranked[0].Fingerprint)
	}
	if ranked[1].Fingerprint != "medium-mid-savings" {
		t.Errorf("expected second-highest savings*confidence second, got %q", ranked[1].Fingerprint)
	}
	if ranked[2].Fingerprint != "high-small-savings" {
		t.Errorf("expected third-highest savings*confidence third, got %q", ranked[2].Fingerprint)
	}
}
