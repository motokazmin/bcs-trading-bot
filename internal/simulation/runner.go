package simulation

import (
	"context"
	"fmt"
	"time"

	"bcs-trading-bot/internal/config"
	"bcs-trading-bot/internal/engine"
	"bcs-trading-bot/internal/position"
	"bcs-trading-bot/internal/risk"
	"bcs-trading-bot/internal/strategy"
	"bcs-trading-bot/internal/trailing"
	"bcs-trading-bot/pkg/interfaces"
	"bcs-trading-bot/pkg/models"
)

// RunnerConfig — параметры симуляции одного тикера на исторических свечах.
type RunnerConfig struct {
	Ticker          string
	ClassCode       string
	CandleTimeframe string
	TradingMode     string
	RunID           string
	ExperimentID    string
	StepPriceValue  float64
	Deposit         float64
	MaxDailyLoss    float64
	RiskPerTradePct float64
	MaxTradesPerDay int
	Strategy        strategy.CandleStrategy
	StrategyID      string
	StopMode        string
	Lookback             int
	TrailCfg             trailing.Config
	StrategyParamsJSON   string
	SessionCfg           config.SessionConfig
}

// Runner воспроизводит торговый цикл воркера на исторических свечах.
type Runner struct {
	cfg     RunnerConfig
	session *engine.SessionClock
	strat   strategy.CandleStrategy
	riskMgr *risk.RiskManager
	store   interfaces.TradeStore

	position      *position.State
	eodCloseDate  string
	riskResetDate string
	tradesToday   int
}

// NewRunner создаёт симулятор для одного тикера.
func NewRunner(cfg RunnerConfig, store interfaces.TradeStore) (*Runner, error) {
	if cfg.StepPriceValue <= 0 {
		cfg.StepPriceValue = 1.0
	}
	if store == nil {
		store = interfaces.NoopTradeStore{}
	}

	clock, err := engine.NewSessionClockExt(
		cfg.SessionCfg.Timezone,
		cfg.SessionCfg.EODCloseTime,
		cfg.SessionCfg.SessionOpenTime,
		cfg.SessionCfg.EntryDelayMinutes,
		cfg.SessionCfg.WeekdaysOnly,
		cfg.SessionCfg.WeekendOnly,
	)
	if err != nil {
		return nil, err
	}

	trailCfg := cfg.TrailCfg
	if trailCfg.StepPriceValue <= 0 {
		trailCfg.StepPriceValue = cfg.StepPriceValue
	}
	cfg.TrailCfg = trailCfg

	if cfg.Strategy == nil {
		return nil, fmt.Errorf("simulation: strategy не задана")
	}

	return &Runner{
		cfg:     cfg,
		session: clock,
		strat:   cfg.Strategy,
		riskMgr: risk.NewRiskManager(cfg.Deposit, cfg.MaxDailyLoss, cfg.RiskPerTradePct, cfg.StepPriceValue),
		store:   store,
	}, nil
}

// Run прогоняет свечи в хронологическом порядке.
func (r *Runner) Run(ctx context.Context, candles []models.Candle, executor interfaces.OrderExecutor) error {
	for _, candle := range candles {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if candle.Ticker == "" {
			candle.Ticker = r.cfg.Ticker
		}
		r.checkDailyReset(candle.Timestamp)
		r.checkEOD(ctx, executor, candle)
		if r.position != nil {
			r.processIntrabar(ctx, executor, candle)
		}
		if r.position == nil {
			r.processCandle(ctx, executor, candle)
		}
	}
	return nil
}

func (r *Runner) processCandle(ctx context.Context, executor interfaces.OrderExecutor, candle models.Candle) {
	if !r.session.EntriesAllowed(candle.Timestamp) {
		return
	}
	if r.cfg.MaxTradesPerDay > 0 && r.tradesToday >= r.cfg.MaxTradesPerDay {
		return
	}

	signal := r.strat.OnCandle(candle)
	if signal == nil {
		return
	}
	if err := r.riskMgr.CheckCircuitBreaker(); err != nil {
		return
	}

	qty := r.riskMgr.CalculatePositionSize(signal.Price, signal.StopLoss)
	if qty <= 0 {
		return
	}
	signal.Quantity = qty
	signal.OrderType = models.OrderTypeLimit

	if err := executor.ExecuteOrder(ctx, *signal); err != nil {
		return
	}

	r.tradesToday++
	r.position = position.NewFromSignal(*signal, candle.Timestamp)
}

func (r *Runner) processIntrabar(ctx context.Context, executor interfaces.OrderExecutor, candle models.Candle) {
	for _, price := range position.IntrabarPrices(candle, r.position.Direction) {
		position.UpdateMFE(r.position, price)
		position.UpdateMAE(r.position, price)
		trailing.Apply(r.position, price, r.cfg.TrailCfg)
		if reason := position.CheckExit(r.position, price); reason != "" {
			r.closePosition(ctx, executor, price, reason, candle.Timestamp)
			return
		}
	}
}

func (r *Runner) checkEOD(ctx context.Context, executor interfaces.OrderExecutor, candle models.Candle) {
	ts := candle.Timestamp
	if !r.session.ShouldForceClose(ts) {
		if r.session.EntriesAllowed(ts) {
			r.eodCloseDate = ""
		}
		return
	}
	today := r.session.Today(ts)
	if r.eodCloseDate == today {
		return
	}
	if r.position != nil {
		r.closePosition(ctx, executor, candle.Close, models.CloseReasonEOD, ts)
	}
	r.eodCloseDate = today
}

func (r *Runner) checkDailyReset(now time.Time) {
	if !r.session.IsSessionOpen(now) {
		return
	}
	today := r.session.Today(now)
	if r.riskResetDate == today {
		return
	}
	r.riskMgr.ResetDaily()
	r.tradesToday = 0
	r.riskResetDate = today
}

func (r *Runner) closePosition(ctx context.Context, executor interfaces.OrderExecutor, price float64, reason string, closedAt time.Time) {
	if r.position == nil {
		return
	}

	pos := r.position
	r.position = nil

	closeDir := "SELL"
	if pos.Direction == "SELL" {
		closeDir = "BUY"
	}

	order := models.Order{
		Ticker:      r.cfg.Ticker,
		Direction:   closeDir,
		Quantity:    pos.Quantity,
		Price:       price,
		OrderType:   models.OrderTypeMarket,
		CloseReason: reason,
	}
	if err := executor.ExecuteOrder(ctx, order); err != nil {
		r.position = pos
		return
	}

	pnl := position.CalcPnL(pos, price, r.cfg.StepPriceValue)
	if pnl < 0 {
		r.riskMgr.RegisterLoss(-pnl)
	}

	riskAmount := pos.RDistance * float64(pos.Quantity) * r.cfg.StepPriceValue
	pnlR := 0.0
	if riskAmount > 0 {
		pnlR = pnl / riskAmount
	}

	trade := models.ClosedTrade{
		TradingMode:       r.cfg.TradingMode,
		RunID:             r.cfg.RunID,
		ExperimentID:      r.cfg.ExperimentID,
		StopMode:          r.cfg.StopMode,
		Ticker:            r.cfg.Ticker,
		ClassCode:         r.cfg.ClassCode,
		StepPriceValue:    r.cfg.StepPriceValue,
		Direction:         pos.Direction,
		Quantity:          pos.Quantity,
		EntryPrice:        pos.EntryPrice,
		ExitPrice:         price,
		InitialStopLoss:   pos.InitialStopLoss,
		InitialTakeProfit: pos.InitialTakeProfit,
		FinalStopLoss:     pos.StopLoss,
		RDistance:         pos.RDistance,
		GrossPnL:          pnl,
		PnLR:              pnlR,
		MFEinR:            position.CalcMFEinR(pos),
		MAEinR:            position.CalcMAEinR(pos),
		BreakoutUpper:     pos.BreakoutUpper,
		BreakoutLower:     pos.BreakoutLower,
		CloseReason:       reason,
		TrailStage:        pos.TrailStage,
		IsWinner:          pnl > 0,
		OpenedAt:          pos.OpenedAt,
		ClosedAt:          closedAt,
		HoldSeconds:       int(closedAt.Sub(pos.OpenedAt).Seconds()),
		TradingDate:       r.session.Today(closedAt),
		CandleTimeframe:   r.cfg.CandleTimeframe,
		Lookback:             r.cfg.Lookback,
		RiskPerTradePct:      r.cfg.RiskPerTradePct,
		DepositPerTicker:     r.cfg.Deposit,
		StrategyParamsJSON:   r.cfg.StrategyParamsJSON,
	}
	_ = r.store.SaveClosedTrade(ctx, trade)
}
