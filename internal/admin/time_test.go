package admin

import (
	"testing"
	"time"
)

func TestFormatAdminTimeMSK(t *testing.T) {
	// В БД хранится московское время, просто форматируем
	msk := time.FixedZone("MSK", 3*3600)
	ts := time.Date(2026, 7, 20, 21, 15, 0, 0, msk)
	got := formatAdminTimeMSK(ts)
	want := "20.07 21:15"
	if got != want {
		t.Fatalf("fmtTime: got %q, want %q", got, want)
	}
	if formatAdminTimeMSK(time.Time{}) != "—" {
		t.Fatal("zero time should be em dash")
	}
}
