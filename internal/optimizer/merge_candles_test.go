package optimizer_test

import (
	"testing"
	"time"

	"bcs-trading-bot/internal/optimizer"
	"bcs-trading-bot/pkg/models"
)

func TestMergeCandles(t *testing.T) {
	t0 := time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC)
	t1 := t0.Add(5 * time.Minute)
	t2 := t1.Add(5 * time.Minute)

	existing := []models.Candle{
		{Timestamp: t0, Close: 100},
		{Timestamp: t1, Close: 101},
	}
	fresh := []models.Candle{
		{Timestamp: t1, Close: 999}, // дубликат — должен остаться первый
		{Timestamp: t2, Close: 102},
	}

	merged := optimizer.MergeCandles(existing, fresh)
	if len(merged) != 3 {
		t.Fatalf("len: got %d, want 3", len(merged))
	}
	if merged[1].Close != 101 {
		t.Fatalf("dedupe: got close %.2f, want 101", merged[1].Close)
	}
	if merged[2].Close != 102 {
		t.Fatalf("last close: got %.2f", merged[2].Close)
	}
}
