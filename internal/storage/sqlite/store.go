package sqlite

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"bcs-trading-bot/pkg/models"

	_ "modernc.org/sqlite"
)

//go:embed migrations/001_closed_trades.sql
var migration001 string

const timeLayout = "2006-01-02 15:04:05"

// Store сохраняет закрытые сделки в SQLite.
type Store struct {
	db *sql.DB
}

// Open открывает (или создаёт) файл БД и применяет миграции.
func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("создание каталога для БД: %w", err)
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("открытие sqlite %q: %w", path, err)
	}

	db.SetMaxOpenConns(1)

	if _, err := db.Exec(`PRAGMA journal_mode=WAL`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("WAL mode: %w", err)
	}

	if err := applyMigrations(db); err != nil {
		_ = db.Close()
		return nil, err
	}

	return &Store{db: db}, nil
}

func (s *Store) SaveClosedTrade(_ context.Context, trade models.ClosedTrade) error {
	isWinner := 0
	if trade.IsWinner {
		isWinner = 1
	}

	experimentID := trade.ExperimentID
	if experimentID == "" {
		experimentID = "default"
	}
	stopMode := trade.StopMode
	if stopMode == "" {
		stopMode = "range"
	}

	now := time.Now().Format(timeLayout)

	_, err := s.db.Exec(`
		INSERT INTO closed_trades (
			trading_mode, run_id, experiment_id, stop_mode, recorded_at,
			ticker, class_code, step_price_value,
			direction, quantity,
			entry_price, exit_price,
			initial_stop_loss, initial_take_profit, final_stop_loss, r_distance,
			gross_pnl, pnl_r,
			close_reason, trail_stage, is_winner,
			opened_at, closed_at, hold_seconds, trading_date,
			candle_timeframe, lookback, risk_per_trade_pct, deposit_per_ticker
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		trade.TradingMode,
		trade.RunID,
		experimentID,
		stopMode,
		now,
		trade.Ticker,
		trade.ClassCode,
		trade.StepPriceValue,
		trade.Direction,
		trade.Quantity,
		trade.EntryPrice,
		trade.ExitPrice,
		trade.InitialStopLoss,
		trade.InitialTakeProfit,
		trade.FinalStopLoss,
		trade.RDistance,
		trade.GrossPnL,
		trade.PnLR,
		trade.CloseReason,
		trade.TrailStage,
		isWinner,
		trade.OpenedAt.Format(timeLayout),
		trade.ClosedAt.Format(timeLayout),
		trade.HoldSeconds,
		trade.TradingDate,
		trade.CandleTimeframe,
		trade.Lookback,
		trade.RiskPerTradePct,
		trade.DepositPerTicker,
	)
	if err != nil {
		return fmt.Errorf("insert closed_trade: %w", err)
	}
	return nil
}

func (s *Store) Close() error {
	if s.db == nil {
		return nil
	}
	return s.db.Close()
}
