package optimizer_test

import (
	"context"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
	"time"

	"bcs-trading-bot/internal/optimizer"
	"bcs-trading-bot/pkg/models"
)

func TestPipelineIntegration(t *testing.T) {
	dir := t.TempDir()
	candles := syntheticCandles("TEST", 5*24*12) // ~5 дней M5
	path := filepath.Join(dir, "TEST.csv")
	if err := optimizer.WriteCSV(path, candles); err != nil {
		t.Fatalf("write csv: %v", err)
	}

	spaceYAML := `
strategy: momentum_breakout
parameters:
  lookback:
    type: int
    min: 10
    max: 12
  breakoutThreshold:
    type: float
    min: 0.0
    max: 0.1
  volumeFilterMultiplier:
    type: float
    min: 1.0
    max: 1.5
  atrMultiplier:
    type: float
    min: 1.5
    max: 2.0
  trailActivationR:
    type: float
    min: 1.0
    max: 1.5
  trailDiscreteStepR:
    type: float
    min: 0.5
    max: 1.0
  trailStageMax:
    type: int
    min: 1
    max: 2
  maxEntriesPerTickerPerDay:
    type: int
    min: 2
    max: 3
fixed:
  riskPerTradePercent: 0.5
  dailyLossLimitPercent: 2.0
`
	spacePath := filepath.Join(dir, "space.yaml")
	if err := os.WriteFile(spacePath, []byte(spaceYAML), 0o644); err != nil {
		t.Fatal(err)
	}

	space, err := optimizer.LoadSearchSpace(spacePath)
	if err != nil {
		t.Fatalf("load space: %v", err)
	}

	data, err := optimizer.LoadCandleData(dir, []string{"TEST"})
	if err != nil {
		t.Fatalf("load candles: %v", err)
	}

	from := candles[0].Timestamp
	to := candles[len(candles)-1].Timestamp.Add(time.Minute)

	settings := optimizer.RunSettings{
		Tickers:            []string{"TEST"},
		StopMode:           "atr",
		ClassCode:          "TQBR",
		CandleTimeframe:    "M5",
		Deposit:            100_000,
		StepPriceValue:     1.0,
		CommissionPerTrade: 5.0,
		MinTrades:          1,
		Session:            optimizer.DefaultSession(),
	}

	evaluator := optimizer.NewEvaluator(settings, space, data)
	windows := optimizer.GenerateWindows(from, to.Add(24*time.Hour), 1, 1, 1)
	if len(windows) == 0 {
		// fallback: один синтетический window
		windows = []optimizer.Window{{
			TrainStart: from,
			TrainEnd:   from.Add(48 * time.Hour),
			TestStart:  from.Add(48 * time.Hour),
			TestEnd:    to,
		}}
	}
	evaluator.PrecomputeWindowSlices(windows)

	result := optimizer.RunOptimization(context.Background(), evaluator, space, windows, 3, 1, rand.Int63())
	if len(result.Trials) != 3 {
		t.Fatalf("trials: got %d", len(result.Trials))
	}

	searcher := optimizer.NewRandomSearcher(space, 3, 42)
	for _, tr := range result.Trials {
		searcher.Report(tr.Params, tr.TrainScore)
	}
	best, score := searcher.Best()
	if len(best) == 0 && !mathIsInf(score) {
		t.Fatalf("unexpected best score %v", score)
	}
}

func mathIsInf(f float64) bool {
	return f != f || f > 1e100 || f < -1e100
}

func syntheticCandles(ticker string, n int) []models.Candle {
	start := time.Date(2024, 6, 1, 10, 0, 0, 0, time.UTC)
	price := 100.0
	out := make([]models.Candle, 0, n)
	for i := 0; i < n; i++ {
		// тренд с периодическими пробоями
		if i%50 == 0 {
			price += 3
		} else if i%30 == 0 {
			price -= 2
		}
		high := price + 1
		low := price - 1
		out = append(out, models.Candle{
			Ticker:    ticker,
			Open:      price,
			High:      high,
			Low:       low,
			Close:     price + 0.2,
			Volume:    1000 + int64(i%100),
			Timestamp: start.Add(time.Duration(i*5) * time.Minute),
		})
		price = out[len(out)-1].Close
	}
	return out
}
