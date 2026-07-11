package eval

import (
	"testing"
	"time"

	"bcs-trading-bot/internal/optimizer/core"
	"bcs-trading-bot/pkg/models"
)

func TestBuildWindowCandleSlices(t *testing.T) {
	t0 := time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC)
	candles := []models.Candle{
		{Timestamp: t0, Close: 1},
		{Timestamp: t0.Add(24 * time.Hour), Close: 2},
		{Timestamp: t0.Add(48 * time.Hour), Close: 3},
		{Timestamp: t0.Add(72 * time.Hour), Close: 4},
	}
	data := map[string][]models.Candle{"SBER": candles}
	windows := []core.Window{{
		Start: t0,
		End:   t0.Add(48 * time.Hour),
	}}

	slices := BuildWindowCandleSlices(windows, []string{"SBER"}, data)
	if len(slices) != 1 {
		t.Fatalf("windows: got %d", len(slices))
	}
	if len(slices[0].Candles["SBER"]) != 2 {
		t.Fatalf("window candles: got %d want 2", len(slices[0].Candles["SBER"]))
	}
}
