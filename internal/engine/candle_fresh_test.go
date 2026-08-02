package engine

import (
	"testing"
	"time"
)

func TestCandleBarDuration(t *testing.T) {
	cases := []struct {
		tf   string
		want time.Duration
	}{
		{"M1", time.Minute},
		{"M5", 5 * time.Minute},
		{"", 5 * time.Minute},
		{"m15", 15 * time.Minute},
		{"M30", 30 * time.Minute},
		{"H1", time.Hour},
		{"weird", 5 * time.Minute},
	}
	for _, tc := range cases {
		if got := candleBarDuration(tc.tf); got != tc.want {
			t.Fatalf("candleBarDuration(%q)=%v, want %v", tc.tf, got, tc.want)
		}
	}
}

func TestCandleMaxAgeM5(t *testing.T) {
	want := 15 * time.Minute
	if got := candleMaxAge("M5"); got != want {
		t.Fatalf("candleMaxAge(M5)=%v, want %v", got, want)
	}
}

func TestCandleFresh(t *testing.T) {
	now := time.Date(2026, 8, 2, 8, 20, 0, 0, time.UTC)
	maxAge := 15 * time.Minute

	fresh := now.Add(-10 * time.Minute)
	if !candleFresh(now, fresh, maxAge) {
		t.Fatal("10m old bar should be fresh for 15m maxAge")
	}

	stale := now.Add(-16 * time.Minute)
	if candleFresh(now, stale, maxAge) {
		t.Fatal("16m old bar should be stale for 15m maxAge")
	}

	// Friday bar on Sunday — incident pattern.
	fri := time.Date(2026, 7, 31, 19, 5, 0, 0, time.UTC)
	sun := time.Date(2026, 8, 2, 8, 30, 0, 0, time.UTC)
	if candleFresh(sun, fri, candleMaxAge("M5")) {
		t.Fatal("Friday evening bar must be stale on Sunday morning")
	}

	future := now.Add(2 * time.Minute)
	if !candleFresh(now, future, maxAge) {
		t.Fatal("small clock skew into the future should still be fresh")
	}
}

func TestWeekdaysOnlyBlocksSundayWallClock(t *testing.T) {
	clock, err := NewSessionClockExt("Europe/Moscow", "23:50", "19:05", 0, true, false)
	if err != nil {
		t.Fatal(err)
	}
	loc, err := time.LoadLocation("Europe/Moscow")
	if err != nil {
		t.Fatal(err)
	}
	// Friday bar time would pass EntriesAllowed if used as now — wall clock Sunday must not.
	friBar := time.Date(2026, 7, 31, 19, 5, 0, 0, loc)
	sunNow := time.Date(2026, 8, 2, 8, 30, 0, 0, loc)
	if !clock.EntriesAllowed(friBar) {
		t.Fatal("sanity: Friday evening bar time is within evening session")
	}
	if clock.EntriesAllowed(sunNow) {
		t.Fatal("Sunday wall clock must be blocked by weekdays_only")
	}
}
