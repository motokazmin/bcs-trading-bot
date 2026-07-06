package optimizer_test

import (
	"os"
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

func TestLoadUniverse(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/universe.yaml"
	content := `class_code: TQBR
candle_timeframe: M5
initial_history_years: 2
tickers: [SBER, GAZP]
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	u, err := optimizer.LoadUniverse(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(u.Tickers) != 2 {
		t.Fatalf("tickers: got %d", len(u.Tickers))
	}
	if got := u.ResolveTickers("ROSN,SBER"); len(got) != 2 || got[0] != "ROSN" {
		t.Fatalf("override: got %v", got)
	}
}

func TestCandleDataRange(t *testing.T) {
	t0 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	t1 := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)
	data := map[string][]models.Candle{
		"A": {{Timestamp: t0}, {Timestamp: t1}},
		"B": {{Timestamp: t0.Add(24 * time.Hour)}},
	}
	from, to, ok := optimizer.CandleDataRange(data)
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
