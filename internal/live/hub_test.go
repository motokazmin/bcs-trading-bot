package live

import (
	"testing"
	"time"

	"bcs-trading-bot/pkg/models"
)

type stubSource struct {
	pos *OpenPosition
}

func (s stubSource) Label() string                    { return "x" }
func (s stubSource) Ticker() string                   { return s.pos.Ticker }
func (s stubSource) ExperimentID() string             { return s.pos.ExperimentID }
func (s stubSource) SnapshotPosition() *OpenPosition  { return s.pos }

func TestHubCandlesResetOnNewDay(t *testing.T) {
	h := NewHub()
	day1 := time.Date(2026, 7, 22, 10, 0, 0, 0, time.FixedZone("MSK", 3*3600))
	day2 := time.Date(2026, 7, 23, 10, 0, 0, 0, time.FixedZone("MSK", 3*3600))

	h.IngestCandle(models.Candle{Ticker: "SBER", Open: 1, High: 2, Low: 1, Close: 1.5, Timestamp: day1})
	h.IngestCandle(models.Candle{Ticker: "SBER", Open: 1.5, High: 2, Low: 1, Close: 1.8, Timestamp: day1.Add(5 * time.Minute)})
	if got := len(h.Candles("SBER")); got != 2 {
		t.Fatalf("day1 candles: %d", got)
	}

	h.IngestCandle(models.Candle{Ticker: "SBER", Open: 3, High: 4, Low: 3, Close: 3.5, Timestamp: day2})
	if got := len(h.Candles("SBER")); got != 1 {
		t.Fatalf("day2 reset: %d", got)
	}
}

func TestBuildOpenChartPayload(t *testing.T) {
	opened := time.Date(2026, 7, 22, 8, 0, 0, 0, time.UTC)
	pos := &OpenPosition{
		ID: "a/SBER", ExperimentID: "a", Ticker: "SBER", Direction: "BUY",
		Quantity: 1, EntryPrice: 100, StopLoss: 95, TakeProfit: 110,
		OpenedAt: opened, LastPrice: 101, UnrealizedPnL: 1,
	}
	candles := []models.Candle{{
		Ticker: "SBER", Open: 99, High: 102, Low: 98, Close: 101, Timestamp: opened,
	}}
	payload := BuildOpenChartPayload(candles, pos)
	markers, _ := payload["markers"].([]map[string]any)
	if len(markers) != 1 {
		t.Fatalf("markers: %v", payload["markers"])
	}
	levels, _ := payload["levels"].([]map[string]any)
	if len(levels) != 3 {
		t.Fatalf("levels: %d", len(levels))
	}
}

func TestHubPositions(t *testing.T) {
	h := NewHub()
	h.Register(stubSource{pos: &OpenPosition{
		ID: "e/T", Ticker: "T", ExperimentID: "e", Direction: "BUY",
		EntryPrice: 10, Quantity: 2, LastPrice: 11, UnrealizedPnL: 2,
	}})
	h.IngestTick(models.Tick{Ticker: "T", Price: 12})
	got := h.Positions()
	if len(got) != 1 || got[0].LastPrice != 12 {
		t.Fatalf("%+v", got)
	}
}
