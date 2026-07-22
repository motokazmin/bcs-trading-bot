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

	msk := time.FixedZone("MSK", 3*3600)
	// 13:15 MSK — в БД должно лечь московское время для удобства просмотра.
	openedAt := time.Date(2026, 6, 25, 13, 15, 0, 0, msk)
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
		MAEinR:            0.8,
		BreakoutUpper:     104.5,
		BreakoutLower:     98.0,
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
		tradingMode   string
		experimentID  string
		stopMode      string
		ticker        string
		grossPnL      float64
		trailStage    int
		mfeInR        float64
		maeInR        float64
		breakoutUpper float64
		breakoutLower float64
		openedAt      string
		closedAt      string
	}
	err = store.db.QueryRow(`
		SELECT trading_mode, experiment_id, stop_mode, ticker, gross_pnl, trail_stage,
		       mfe_in_r, mae_in_r, breakout_upper, breakout_lower, opened_at, closed_at
		FROM closed_trades WHERE id = 1`,
	).Scan(&got.tradingMode, &got.experimentID, &got.stopMode, &got.ticker, &got.grossPnL, &got.trailStage,
		&got.mfeInR, &got.maeInR, &got.breakoutUpper, &got.breakoutLower, &got.openedAt, &got.closedAt)
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if got.tradingMode != "virtual" || got.experimentID != "baseline" || got.stopMode != "range" ||
		got.ticker != "SBER" || got.grossPnL != 30.0 || got.trailStage != 1 || got.mfeInR != 2.2 ||
		got.maeInR != 0.8 || got.breakoutUpper != 104.5 || got.breakoutLower != 98.0 {
		t.Fatalf("unexpected row: %+v", got)
	}
	if got.openedAt != "2026-06-25 13:15:00" {
		t.Fatalf("opened_at MSK: got %q, want 2026-06-25 13:15:00", got.openedAt)
	}
	if got.closedAt != "2026-06-25 13:20:00" {
		t.Fatalf("closed_at MSK: got %q, want 2026-06-25 13:20:00", got.closedAt)
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

func TestMigration005NormalizesLocalClosedAt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "trades.db")

	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	if _, err := store.db.Exec(`DROP TABLE IF EXISTS _migration_005_utc_times`); err != nil {
		t.Fatalf("drop marker: %v", err)
	}

	// Старый баг: opened UTC, closed Local MSK (+3h), hold корректный.
	_, err = store.db.Exec(`
		INSERT INTO closed_trades (
			trading_mode, run_id, experiment_id, stop_mode, recorded_at,
			ticker, class_code, step_price_value, direction, quantity,
			entry_price, exit_price, initial_stop_loss, initial_take_profit, final_stop_loss, r_distance,
			gross_pnl, pnl_r, close_reason, trail_stage, is_winner,
			opened_at, closed_at, hold_seconds, trading_date,
			candle_timeframe, lookback, risk_per_trade_pct, deposit_per_ticker
		) VALUES (
			'virtual', 'run', 'exp', 'atr', '2026-07-20 22:42:55',
			'CHMF', 'TQBR', 1, 'BUY', 1,
			100, 101, 99, 102, 99, 1,
			1, 1, 'TP', 0, 1,
			'2026-07-20 18:15:00', '2026-07-20 22:42:55', 5275, '2026-07-20',
			'M5', 0, 0.5, 200000
		)`)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := applyMigration005(store.db); err != nil {
		t.Fatalf("migration: %v", err)
	}

	var closedAt, recordedAt string
	if err := store.db.QueryRow(`SELECT closed_at, recorded_at FROM closed_trades WHERE id = 1`).Scan(&closedAt, &recordedAt); err != nil {
		t.Fatalf("select: %v", err)
	}
	// 18:15:00 + 5275s = 19:42:55 UTC
	if closedAt != "2026-07-20 19:42:55" {
		t.Fatalf("closed_at: got %q, want 2026-07-20 19:42:55", closedAt)
	}
	if recordedAt != "2026-07-20 19:42:55" {
		t.Fatalf("recorded_at: got %q, want 2026-07-20 19:42:55", recordedAt)
	}

	if err := applyMigration005(store.db); err != nil {
		t.Fatalf("idempotent migration: %v", err)
	}
}

func TestNormalizeClosedAtSkewHealsNewLocalRows(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "trades.db")

	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	// Симулируем запись старым бинарником после миграции 005.
	_, err = store.db.Exec(`
		INSERT INTO closed_trades (
			trading_mode, run_id, experiment_id, stop_mode, recorded_at,
			ticker, class_code, step_price_value, direction, quantity,
			entry_price, exit_price, initial_stop_loss, initial_take_profit, final_stop_loss, r_distance,
			gross_pnl, pnl_r, close_reason, trail_stage, is_winner,
			opened_at, closed_at, hold_seconds, trading_date,
			candle_timeframe, lookback, risk_per_trade_pct, deposit_per_ticker
		) VALUES (
			'virtual', 'run', 'exp', 'atr', '2026-07-21 07:46:16',
			'CHMF', 'TQBR', 1, 'BUY', 1,
			100, 101, 99, 102, 99, 1,
			1, 1, 'SL', 0, 0,
			'2026-07-21 04:40:00', '2026-07-21 07:46:16', 376, '2026-07-21',
			'M5', 0, 0.5, 200000
		)`)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := normalizeClosedAtSkew(store.db); err != nil {
		t.Fatalf("normalize: %v", err)
	}

	var closedAt string
	var diff int
	if err := store.db.QueryRow(`
		SELECT closed_at, (strftime('%s', closed_at) - strftime('%s', opened_at))
		FROM closed_trades WHERE id = 1`).Scan(&closedAt, &diff); err != nil {
		t.Fatalf("select: %v", err)
	}
	if closedAt != "2026-07-21 04:46:16" {
		t.Fatalf("closed_at: got %q, want 2026-07-21 04:46:16", closedAt)
	}
	if diff != 376 {
		t.Fatalf("diff: got %d, want 376", diff)
	}
}
