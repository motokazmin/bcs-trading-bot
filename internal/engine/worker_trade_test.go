package engine

import (
	"context"
	"sync"
	"testing"
	"time"

	"bcs-trading-bot/internal/costs"
	"bcs-trading-bot/internal/config"
	"bcs-trading-bot/internal/position"
	"bcs-trading-bot/internal/risk"
	"bcs-trading-bot/internal/strategy"
	"bcs-trading-bot/pkg/interfaces"
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

func (stubExecutor) ExecuteOrder(context.Context, models.Order) error { return nil }
func (stubExecutor) GetBalance(context.Context) (float64, error)     { return 0, nil }

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
		1.0,
		costs.Config{CommissionPerLot: 0.10},
		config.SessionConfig{Timezone: "Europe/Moscow", EODCloseTime: "23:40", SessionOpenTime: "10:00"},
		config.TradingModeVirtual,
		"test-run",
		"TQBR",
		"M5",
		store,
		nil,
	)
	if err != nil {
		t.Fatalf("NewTickerWorker: %v", err)
	}

	openedAt := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)
	worker.position = &position.State{
		Direction:         "BUY",
		Quantity:          10,
		EntryPrice:        100,
		InitialStopLoss:   95,
		InitialTakeProfit: 115,
		StopLoss:          100,
		TakeProfit:        115,
		RDistance:         5,
		TrailStage:        1,
		MFEPrice:          110,
		BreakoutUpper:     101,
		BreakoutLower:     99,
		OpenedAt:          openedAt,
	}

	worker.closePosition(context.Background(), stubExecutor{}, 110, models.CloseReasonTakeProfit)

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
	if tr.GrossPnL != 149 {
		t.Fatalf("net pnl: got %.2f, want 149 (TP fill at level 115, commission 1)", tr.GrossPnL)
	}
	if tr.PnLR != 2.98 {
		t.Fatalf("pnl_r: got %.2f, want 2.98", tr.PnLR)
	}
	if tr.ExitPrice != 115 {
		t.Fatalf("exit_price: got %.2f, want 115 (TP level)", tr.ExitPrice)
	}
	if tr.CloseReason != models.CloseReasonTakeProfit {
		t.Fatalf("close_reason: got %q", tr.CloseReason)
	}
	if tr.TrailStage != 1 {
		t.Fatalf("trail_stage: got %d", tr.TrailStage)
	}
	if tr.MFEinR != 2 {
		t.Fatalf("mfe_in_r: got %.2f, want 2", tr.MFEinR)
	}
	if tr.BreakoutUpper != 101 || tr.BreakoutLower != 99 {
		t.Fatalf("breakout: upper=%.2f lower=%.2f", tr.BreakoutUpper, tr.BreakoutLower)
	}
}

type countingExecutor struct {
	calls int
}

func (e *countingExecutor) ExecuteOrder(context.Context, models.Order) error {
	e.calls++
	return nil
}

func (e *countingExecutor) GetBalance(context.Context) (float64, error) { return 0, nil }

func TestClosePositionIgnoresSecondCall(t *testing.T) {
	store := &recordingTradeStore{}
	exp := config.ResolvedExperiment{
		ID:       "baseline",
		Strategy: config.StrategyConfig{Lookback: 20, StopMode: strategy.StopModeRange},
		Risk:     config.RiskConfig{Deposit: 100_000, MaxDailyLoss: 2_000, RiskPerTradePercent: 0.5},
	}
	worker, err := NewTickerWorker(
		"SBER", exp, 1.0, costs.Config{CommissionPerLot: 0.10},
		config.SessionConfig{Timezone: "Europe/Moscow", EODCloseTime: "23:40", SessionOpenTime: "10:00"},
		config.TradingModeVirtual, "test-run", "TQBR", "M5", store,
		nil,
	)
	if err != nil {
		t.Fatalf("NewTickerWorker: %v", err)
	}
	worker.position = &position.State{
		Direction:  "BUY",
		Quantity:   1,
		EntryPrice: 100,
		StopLoss:   95,
		RDistance:  5,
		OpenedAt:   time.Now(),
	}

	exec := &countingExecutor{}
	ctx := context.Background()
	worker.closePosition(ctx, exec, 110, models.CloseReasonTakeProfit)
	worker.closePosition(ctx, exec, 110, models.CloseReasonTakeProfit)

	if exec.calls != 1 {
		t.Fatalf("ExecuteOrder calls: got %d, want 1", exec.calls)
	}
	if worker.position != nil {
		t.Fatal("position should stay nil after successful close")
	}
}

func TestClosePositionGhostDropsWithoutRestore(t *testing.T) {
	store := &recordingTradeStore{}
	exp := config.ResolvedExperiment{
		ID:       "or-fade",
		Strategy: config.StrategyConfig{Lookback: 20, StopMode: strategy.StopModeRange},
		Risk:     config.RiskConfig{Deposit: 100_000, MaxDailyLoss: 2_000, RiskPerTradePercent: 0.5},
	}
	globalRisk := risk.NewGlobalRiskController(200_000, 2.0, 4)
	worker, err := NewTickerWorker(
		"CHMF", exp, 1.0, costs.Config{},
		config.SessionConfig{Timezone: "Europe/Moscow", EODCloseTime: "23:40", SessionOpenTime: "10:00"},
		config.TradingModeVirtual, "test-run", "TQBR", "M5", store, globalRisk,
	)
	if err != nil {
		t.Fatalf("NewTickerWorker: %v", err)
	}
	if err := globalRisk.TryOpen("CHMF", 1000); err != nil {
		t.Fatalf("TryOpen: %v", err)
	}
	worker.position = &position.State{
		Direction:  "BUY",
		Quantity:   1,
		EntryPrice: 680,
		StopLoss:   677,
		TakeProfit: 684,
		RDistance:  3,
		OpenedAt:   time.Now(),
	}

	worker.closePosition(context.Background(), missingPosExecutor{}, 677, models.CloseReasonStopLoss)

	if worker.position != nil {
		t.Fatal("ghost close must drop local position (no restore/spam)")
	}
	if globalRisk.OpenPositionCount() != 0 {
		t.Fatalf("global risk must release ticker, count=%d", globalRisk.OpenPositionCount())
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.trades) != 0 {
		t.Fatalf("ghost close must not save trade, got %d", len(store.trades))
	}
}

type missingPosExecutor struct{}

func (missingPosExecutor) ExecuteOrder(context.Context, models.Order) error {
	return interfaces.ErrNoOpenPosition
}
func (missingPosExecutor) GetBalance(context.Context) (float64, error) { return 0, nil }

func TestCheckSLTPStopLossFillsAtStopLevel(t *testing.T) {
	store := &recordingTradeStore{}
	exp := config.ResolvedExperiment{
		ID:       "orc-wave2",
		Strategy: config.StrategyConfig{Lookback: 20, StopMode: strategy.StopModeATR},
		Risk:     config.RiskConfig{Deposit: 200_000, MaxDailyLoss: 4_000, RiskPerTradePercent: 0.5},
	}
	worker, err := NewTickerWorker(
		"TATN", exp, 1.0, costs.Config{},
		config.SessionConfig{Timezone: "Europe/Moscow", EODCloseTime: "18:40", SessionOpenTime: "10:00"},
		config.TradingModeVirtual, "test-run", "TQBR", "M5", store, nil,
	)
	if err != nil {
		t.Fatalf("NewTickerWorker: %v", err)
	}
	worker.position = &position.State{
		Direction:         "SELL",
		Quantity:          407,
		EntryPrice:        484.7,
		InitialStopLoss:   485.84,
		InitialTakeProfit: 482.83,
		StopLoss:          485.84,
		TakeProfit:        482.83,
		RDistance:         1.14,
		MFEPrice:          484.7,
		MAEPrice:          484.7,
		OpenedAt:          time.Now(),
	}

	worker.checkSLTP(context.Background(), stubExecutor{}, 491.7)

	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.trades) != 1 {
		t.Fatalf("trades: got %d, want 1", len(store.trades))
	}
	tr := store.trades[0]
	if tr.CloseReason != models.CloseReasonStopLoss {
		t.Fatalf("reason: %q", tr.CloseReason)
	}
	if tr.ExitPrice != 485.84 {
		t.Fatalf("exit: got %.2f, want stop 485.84 (not tick 491.7)", tr.ExitPrice)
	}
	if tr.PnLR >= 0 || tr.PnLR < -1.2 {
		t.Fatalf("pnl_r: got %.3f, want about -1R (filled at stop)", tr.PnLR)
	}
}
