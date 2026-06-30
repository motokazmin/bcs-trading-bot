package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"bcs-trading-bot/pkg/models"
)

func TestStoreSaveClosedTrade(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(filepath.Join(dir, "trades.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	openedAt := time.Date(2026, 6, 25, 10, 15, 0, 0, time.UTC)
	closedAt := openedAt.Add(5 * time.Minute)

	trade := models.ClosedTrade{
		TradingMode:       "virtual",
		RunID:             "test-run",
		ExperimentID:      "baseline",
		StopMode:          "range",
		Ticker:            "SBER",
		ClassCode:         "TQBR",
		StepPriceValue:    1.0,
		Direction:         "BUY",
		Quantity:          10,
		EntryPrice:        300.0,
		ExitPrice:         303.0,
		InitialStopLoss:   298.0,
		InitialTakeProfit: 306.0,
		FinalStopLoss:     300.0,
		RDistance:         2.0,
		GrossPnL:          30.0,
		PnLR:              1.5,
		MFEinR:            2.2,
		CloseReason:       models.CloseReasonTakeProfit,
		TrailStage:        1,
		IsWinner:          true,
		OpenedAt:          openedAt,
		ClosedAt:          closedAt,
		HoldSeconds:       300,
		TradingDate:       "2026-06-25",
		CandleTimeframe:   "M5",
		Lookback:          20,
		RiskPerTradePct:   0.5,
		DepositPerTicker:  100000,
	}

	if err := store.SaveClosedTrade(context.Background(), trade); err != nil {
		t.Fatalf("SaveClosedTrade: %v", err)
	}

	var count int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM closed_trades`).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("count: got %d, want 1", count)
	}

	var got struct {
		tradingMode  string
		experimentID string
		stopMode     string
		ticker       string
		grossPnL     float64
		trailStage   int
		mfeInR       float64
	}
	err = store.db.QueryRow(`
		SELECT trading_mode, experiment_id, stop_mode, ticker, gross_pnl, trail_stage, mfe_in_r
		FROM closed_trades WHERE id = 1`,
	).Scan(&got.tradingMode, &got.experimentID, &got.stopMode, &got.ticker, &got.grossPnL, &got.trailStage, &got.mfeInR)
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if got.tradingMode != "virtual" || got.experimentID != "baseline" || got.stopMode != "range" ||
		got.ticker != "SBER" || got.grossPnL != 30.0 || got.trailStage != 1 || got.mfeInR != 2.2 {
		t.Fatalf("unexpected row: %+v", got)
	}
}

func TestOpenIdempotentMigrations(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "trades.db")

	store1, err := Open(path)
	if err != nil {
		t.Fatalf("first open: %v", err)
	}
	store1.Close()

	store2, err := Open(path)
	if err != nil {
		t.Fatalf("second open: %v", err)
	}
	store2.Close()
}
