package api

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bcs-trading-bot/internal/storage/sqlite"
	"bcs-trading-bot/pkg/models"
)

func seedTrades(t *testing.T) *sqlite.Store {
	t.Helper()
	dir := t.TempDir()
	store, err := sqlite.Open(filepath.Join(dir, "trades.db"))
	if err != nil {
		t.Fatal(err)
	}

	opened := time.Date(2026, 6, 25, 10, 0, 0, 0, time.UTC)
	trades := []models.ClosedTrade{
		{
			TradingMode: "virtual", RunID: "run-1", ExperimentID: "baseline", StopMode: "range",
			Ticker: "SBER", ClassCode: "TQBR", StepPriceValue: 1,
			Direction: "BUY", Quantity: 10, EntryPrice: 300, ExitPrice: 303,
			InitialStopLoss: 298, InitialTakeProfit: 306, FinalStopLoss: 300, RDistance: 2,
			GrossPnL: 30, PnLR: 1.5, CloseReason: models.CloseReasonTakeProfit,
			TrailStage: 1, IsWinner: true, OpenedAt: opened, ClosedAt: opened.Add(5 * time.Minute),
			HoldSeconds: 300, TradingDate: "2026-06-25", CandleTimeframe: "M5", Lookback: 20,
			RiskPerTradePct: 0.5, DepositPerTicker: 20000,
		},
		{
			TradingMode: "virtual", RunID: "run-1", ExperimentID: "atr-2", StopMode: "atr",
			Ticker: "SBER", ClassCode: "TQBR", StepPriceValue: 1,
			Direction: "BUY", Quantity: 5, EntryPrice: 300, ExitPrice: 295,
			InitialStopLoss: 290, InitialTakeProfit: 330, FinalStopLoss: 290, RDistance: 10,
			GrossPnL: -25, PnLR: -0.5, CloseReason: models.CloseReasonStopLoss,
			TrailStage: 0, IsWinner: false, OpenedAt: opened, ClosedAt: opened.Add(10 * time.Minute),
			HoldSeconds: 600, TradingDate: "2026-06-25", CandleTimeframe: "M5", Lookback: 20,
			RiskPerTradePct: 0.5, DepositPerTicker: 20000,
		},
	}

	for _, tr := range trades {
		if err := store.SaveClosedTrade(context.Background(), tr); err != nil {
			t.Fatal(err)
		}
	}
	return store
}

func TestBuildExportSummary(t *testing.T) {
	store := seedTrades(t)
	defer store.Close()

	svc := NewExportService(store)
	data, err := svc.BuildExportData(context.Background(), models.TradeFilter{}, ExportModeSummary)
	if err != nil {
		t.Fatal(err)
	}

	if data.ExportMode != "summary" {
		t.Fatalf("mode: %q", data.ExportMode)
	}
	if len(data.Experiments) != 2 {
		t.Fatalf("experiments: got %d, want 2", len(data.Experiments))
	}
	for _, exp := range data.Experiments {
		if len(exp.Trades) != 0 {
			t.Fatalf("summary should not include trades for %s", exp.ExperimentID)
		}
	}

	prompt, err := svc.BuildPrompt(context.Background(), models.TradeFilter{}, ExportModeSummary)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt, "data-summary.json") {
		t.Fatal("summary prompt should reference data-summary.json")
	}
	if strings.Contains(prompt, `"trade_count"`) {
		t.Fatal("prompt should not embed JSON data")
	}
}

func TestBuildExportDetailed(t *testing.T) {
	store := seedTrades(t)
	defer store.Close()

	svc := NewExportService(store)
	data, err := svc.BuildExportData(context.Background(), models.TradeFilter{}, ExportModeDetailed)
	if err != nil {
		t.Fatal(err)
	}

	if data.ExportMode != "detailed" {
		t.Fatalf("mode: %q", data.ExportMode)
	}
	totalTrades := 0
	for _, exp := range data.Experiments {
		totalTrades += len(exp.Trades)
	}
	if totalTrades != 2 {
		t.Fatalf("trades: got %d, want 2", totalTrades)
	}

	prompt, err := svc.BuildPrompt(context.Background(), models.TradeFilter{}, ExportModeDetailed)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt, "data-trades.json") {
		t.Fatal("detailed prompt should reference data-trades.json")
	}
}

func TestGetSummaryAndBreakdown(t *testing.T) {
	store := seedTrades(t)
	defer store.Close()

	summary, err := store.GetSummary(context.Background(), models.TradeFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if summary.TradeCount != 2 {
		t.Fatalf("trade count: %d", summary.TradeCount)
	}
	if summary.TotalPnL != 5 {
		t.Fatalf("total pnl: %.2f", summary.TotalPnL)
	}

	rows, err := store.GetBreakdown(context.Background(), models.TradeFilter{}, "experiment_id")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("breakdown rows: %d", len(rows))
	}
}
