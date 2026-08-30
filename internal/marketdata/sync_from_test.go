package marketdata

import (
	"testing"
	"time"

	"bcs-trading-bot/internal/models"
)

func TestSyncFromModes(t *testing.T) {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	bar := 5 * time.Minute

	from, mode := syncFrom(nil, now, 2, bar)
	if mode != syncInitial {
		t.Fatalf("mode: %s", mode)
	}
	if !from.Equal(now.AddDate(-2, 0, 0)) {
		t.Fatalf("initial from: %v", from)
	}

	last := now.Add(-24 * time.Hour)
	existing := []models.Candle{{Timestamp: last}}
	from, mode = syncFrom(existing, now, 2, bar)
	if mode != syncIncremental {
		t.Fatalf("mode: %s", mode)
	}
	if !from.After(last) {
		t.Fatal("incremental from should be after last")
	}

	_, mode = syncFrom([]models.Candle{{Timestamp: now}}, now, 2, bar)
	if mode != syncSkip {
		t.Fatalf("mode: %s", mode)
	}
}
