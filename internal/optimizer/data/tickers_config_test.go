package data

import (
	"os"
	"testing"
	"time"

	"bcs-trading-bot/pkg/models"
)

func TestLoadTickersConfig(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/tickers.yaml"
	content := `class_code: TQBR
candle_timeframe: M5
initial_history_years: 2
tickers: [SBER, GAZP]
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	u, err := LoadTickersConfig(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(u.Tickers) != 2 {
		t.Fatalf("tickers: got %d", len(u.Tickers))
	}
	if got := u.ResolveTickers("ROSN,SBER"); len(got) != 2 || got[0] != "ROSN" {
		t.Fatalf("override: got %v", got)
	}
	if got := u.CommissionPerLot(-1); got != 0 {
		t.Fatalf("commission default flat: got %v, want 0", got)
	}
	if !u.ResolvedCosts(-1, -1).UsesRate("TQBR") {
		t.Fatal("expected default rate commission model")
	}
}

func TestCandleDataRange(t *testing.T) {
	t0 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	t1 := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)
	candleData := map[string][]models.Candle{
		"A": {{Timestamp: t0}, {Timestamp: t1}},
		"B": {{Timestamp: t0.Add(24 * time.Hour)}},
	}
	from, to, ok := CandleDataRange(candleData)
	if !ok {
		t.Fatal("expected ok")
	}
	if !from.Equal(t0) {
		t.Fatalf("from: %v", from)
	}
	if !to.Equal(t1) {
		t.Fatalf("to: %v", to)
	}
}
