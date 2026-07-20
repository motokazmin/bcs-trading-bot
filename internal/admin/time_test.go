package admin

import (
	"testing"
	"time"
)

func TestFormatAdminTimeMSK(t *testing.T) {
	utc := time.Date(2026, 7, 20, 18, 15, 0, 0, time.UTC)
	got := formatAdminTimeMSK(utc)
	want := "20.07 21:15"
	if got != want {
		t.Fatalf("fmtTime: got %q, want %q", got, want)
	}
	if formatAdminTimeMSK(time.Time{}) != "—" {
		t.Fatal("zero time should be em dash")
	}
}
