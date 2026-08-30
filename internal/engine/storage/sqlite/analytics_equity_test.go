package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"bcs-trading-bot/internal/models"
)

func TestGetAccountEquity(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(filepath.Join(dir, "trades.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	msk := time.FixedZone("MSK", 3*3600)
	base := time.Date(2026, 7, 21, 10, 0, 0, 0, msk)

	trades := []models.ClosedTrade{
		{
			TradingMode: "virtual", RunID: "r", ExperimentID: "a", StopMode: "atr",
			Ticker: "SBER", ClassCode: "TQBR", StepPriceValue: 1,
			Direction: "BUY", Quantity: 1, EntryPrice: 100, ExitPrice: 110,
			InitialStopLoss: 95, InitialTakeProfit: 110, FinalStopLoss: 95, RDistance: 5,
			GrossPnL: 1000, PnLR: 1, CloseReason: "TP", IsWinner: true,
			OpenedAt: base, ClosedAt: base.Add(time.Hour), HoldSeconds: 3600,
			TradingDate: "2026-07-21", CandleTimeframe: "M5", DepositPerTicker: 200000,
		},
		{
			TradingMode: "virtual", RunID: "r", ExperimentID: "b", StopMode: "atr",
			Ticker: "GAZP", ClassCode: "TQBR", StepPriceValue: 1,
			Direction: "SELL", Quantity: 1, EntryPrice: 100, ExitPrice: 105,
			InitialStopLoss: 102, InitialTakeProfit: 90, FinalStopLoss: 102, RDistance: 2,
			GrossPnL: -500, PnLR: -1, CloseReason: "SL", IsWinner: false,
			OpenedAt: base.Add(2 * time.Hour), ClosedAt: base.Add(3 * time.Hour), HoldSeconds: 3600,
			TradingDate: "2026-07-21", CandleTimeframe: "M5", DepositPerTicker: 200000,
		},
	}
	for _, tr := range trades {
		if err := store.SaveClosedTrade(context.Background(), tr); err != nil {
			t.Fatal(err)
		}
	}

	eq, err := store.GetAccountEquity(context.Background(), models.TradeFilter{}, 200000)
	if err != nil {
		t.Fatal(err)
	}
	if eq.StartingDeposit != 200000 {
		t.Fatalf("deposit: got %v", eq.StartingDeposit)
	}
	if len(eq.Points) != 2 {
		t.Fatalf("points: %d", len(eq.Points))
	}
	if eq.Points[0].Balance != 201000 {
		t.Fatalf("balance0: %v", eq.Points[0].Balance)
	}
	if eq.CurrentBalance != 200500 {
		t.Fatalf("current: %v", eq.CurrentBalance)
	}

	empty, err := store.GetAccountEquity(context.Background(), models.TradeFilter{Ticker: "NONE"}, 200000)
	if err != nil {
		t.Fatal(err)
	}
	if empty.StartingDeposit != 200000 || len(empty.Points) != 0 || empty.CurrentBalance != 200000 {
		t.Fatalf("empty: %+v", empty)
	}
}
