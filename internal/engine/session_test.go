package engine_test

import (
	"testing"
	"time"

	"bcs-trading-bot/internal/engine"
)

func TestSessionClockEOD(t *testing.T) {
	clock, err := engine.NewSessionClock("Europe/Moscow", "18:40", "10:00", 0)
	if err != nil {
		t.Fatal(err)
	}

	loc, _ := time.LoadLocation("Europe/Moscow")

	before := time.Date(2026, 6, 24, 18, 30, 0, 0, loc)
	if !clock.EntriesAllowed(before) {
		t.Fatal("entries should be allowed at 18:30")
	}
	if clock.ShouldForceClose(before) {
		t.Fatal("EOD should not trigger at 18:30")
	}

	atEOD := time.Date(2026, 6, 24, 18, 40, 0, 0, loc)
	if clock.EntriesAllowed(atEOD) {
		t.Fatal("entries should be blocked at 18:40")
	}
	if !clock.ShouldForceClose(atEOD) {
		t.Fatal("EOD should trigger at 18:40")
	}
}

func TestSessionClockEntryDelay(t *testing.T) {
	clock, err := engine.NewSessionClock("Europe/Moscow", "18:40", "10:00", 30)
	if err != nil {
		t.Fatal(err)
	}

	loc, _ := time.LoadLocation("Europe/Moscow")

	atOpen := time.Date(2026, 6, 24, 10, 15, 0, 0, loc)
	if clock.EntriesAllowed(atOpen) {
		t.Fatal("entries should be blocked at 10:15 with 30 min delay")
	}

	atDelayEnd := time.Date(2026, 6, 24, 10, 30, 0, 0, loc)
	if !clock.EntriesAllowed(atDelayEnd) {
		t.Fatal("entries should be allowed at 10:30 with 30 min delay")
	}
}
