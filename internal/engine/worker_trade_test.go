package engine

import (
	"context"
	"sync"
	"testing"
	"time"

	"bcs-trading-bot/internal/config"
	"bcs-trading-bot/internal/strategy"
	"bcs-trading-bot/pkg/models"
)

type recordingTradeStore struct {
	mu     sync.Mutex
	trades []models.ClosedTrade
}

func (s *recordingTradeStore) SaveClosedTrade(_ context.Context, trade models.ClosedTrade) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.trades = append(s.trades, trade)
	return nil
}

func (s *recordingTradeStore) Close() error { return nil }

type stubExecutor struct{}

func (stubExecutor) ExecuteOrder(models.Order) error { return nil }
func (stubExecutor) GetBalance() (float64, error)  { return 0, nil }

func TestClosePositionSavesTrade(t *testing.T) {
	store := &recordingTradeStore{}
	exp := config.ResolvedExperiment{
		ID:   "baseline",
		Name: "baseline",
		Risk: config.RiskConfig{
			Deposit:             100_000,
			MaxDailyLoss:          2_000,
			RiskPerTradePercent: 0.5,
		},
		Strategy: config.StrategyConfig{Lookback: 20, StopMode: strategy.StopModeRange},
	}
	worker, err := NewTickerWorker(
		"SBER",
		exp,
		1,
		1.0,
		exp.Strategy.StrategyOptions(),
		config.SessionConfig{Timezone: "Europe/Moscow", EODCloseTime: "23:40", SessionOpenTime: "10:00"},
		config.TradingModeVirtual,
		"test-run",
		"TQBR",
		"M5",
		store,
	)
	if err != nil {
		t.Fatalf("NewTickerWorker: %v", err)
	}

	openedAt := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)
	worker.position = &openPosition{
		direction:         "BUY",
		quantity:          10,
		entryPrice:        100,
		initialStopLoss:   95,
		initialTakeProfit: 115,
		stopLoss:          100,
		takeProfit:        115,
		rDistance:         5,
		trailStage:        1,
		openedAt:          openedAt,
	}

	worker.closePosition(stubExecutor{}, 110, models.CloseReasonTakeProfit)

	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.trades) != 1 {
		t.Fatalf("trades saved: got %d, want 1", len(store.trades))
	}

	tr := store.trades[0]
	if tr.TradingMode != config.TradingModeVirtual {
		t.Fatalf("trading_mode: got %q", tr.TradingMode)
	}
	if tr.ExperimentID != "baseline" {
		t.Fatalf("experiment_id: got %q", tr.ExperimentID)
	}
	if tr.StopMode != strategy.StopModeRange {
		t.Fatalf("stop_mode: got %q", tr.StopMode)
	}
	if tr.InitialStopLoss != 95 || tr.FinalStopLoss != 100 {
		t.Fatalf("SL: initial=%.2f final=%.2f", tr.InitialStopLoss, tr.FinalStopLoss)
	}
	if tr.GrossPnL != 100 {
		t.Fatalf("gross_pnl: got %.2f, want 100", tr.GrossPnL)
	}
	if tr.PnLR != 2 {
		t.Fatalf("pnl_r: got %.2f, want 2", tr.PnLR)
	}
	if tr.CloseReason != models.CloseReasonTakeProfit {
		t.Fatalf("close_reason: got %q", tr.CloseReason)
	}
	if tr.TrailStage != 1 {
		t.Fatalf("trail_stage: got %d", tr.TrailStage)
	}
}
