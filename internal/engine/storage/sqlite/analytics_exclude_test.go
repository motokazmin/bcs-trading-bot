package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"bcs-trading-bot/internal/models"
)

// Архивный период вырезается и из списка сделок, и из агрегатов.
func TestExcludeRangesHidesTrades(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(filepath.Join(dir, "trades.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	msk := time.FixedZone("MSK", 3*3600)
	ctx := context.Background()

	save := func(date string, pnl float64) {
		t.Helper()
		day, err := time.ParseInLocation("2006-01-02", date, msk)
		if err != nil {
			t.Fatal(err)
		}
		open := day.Add(11 * time.Hour)
		tr := models.ClosedTrade{
			TradingMode: "virtual", RunID: "r", ExperimentID: "a", StopMode: "atr",
			Ticker: "SBER", ClassCode: "TQBR", StepPriceValue: 1,
			Direction: "BUY", Quantity: 1, EntryPrice: 100, ExitPrice: 110,
			InitialStopLoss: 95, InitialTakeProfit: 110, FinalStopLoss: 95, RDistance: 5,
			GrossPnL: pnl, PnLR: 1, CloseReason: "TP", IsWinner: pnl > 0,
			OpenedAt: open, ClosedAt: open.Add(time.Hour), HoldSeconds: 3600,
			TradingDate: date, CandleTimeframe: "M5", DepositPerTicker: 200000,
		}
		if err := store.SaveClosedTrade(ctx, tr); err != nil {
			t.Fatal(err)
		}
	}

	save("2026-08-15", 100) // внутри архива
	save("2026-08-28", 200) // последний день архива — граница включительно
	save("2026-08-29", 300) // после архива

	archived := models.TradeFilter{
		ExcludeRanges: []models.DateRange{{From: "2026-08-10", To: "2026-08-28"}},
	}

	all, err := store.ListClosedTrades(ctx, models.TradeFilter{}, 100, 0)
	if err != nil {
		t.Fatal(err)
	}
	if all.Total != 3 {
		t.Fatalf("без фильтра: got %d сделок, want 3", all.Total)
	}

	visible, err := store.ListClosedTrades(ctx, archived, 100, 0)
	if err != nil {
		t.Fatal(err)
	}
	if visible.Total != 1 {
		t.Fatalf("с архивом: got %d сделок, want 1", visible.Total)
	}
	if visible.Trades[0].TradingDate != "2026-08-29" {
		t.Fatalf("видна не та сделка: %s", visible.Trades[0].TradingDate)
	}

	summary, err := store.GetSummary(ctx, archived)
	if err != nil {
		t.Fatal(err)
	}
	if summary.TradeCount != 1 {
		t.Fatalf("summary.TradeCount: got %d, want 1", summary.TradeCount)
	}
	if summary.TotalPnL != 300 {
		t.Fatalf("summary.TotalPnL: got %v, want 300 (архивные сделки в агрегате)", summary.TotalPnL)
	}

	dr, err := store.GetDateRange(ctx, archived)
	if err != nil {
		t.Fatal(err)
	}
	if dr.From != "2026-08-29" || dr.To != "2026-08-29" {
		t.Fatalf("GetDateRange: got %+v, want 2026-08-29 — 2026-08-29", dr)
	}
}
