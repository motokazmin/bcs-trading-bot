package simulation

import (
	"context"
	"fmt"
	"sort"
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

// PortfolioRunnerConfig — параметры портфельной симуляции (несколько тикеров, общий риск).
type PortfolioRunnerConfig struct {
	Tickers         map[string]RunnerConfig // ticker -> config
	SessionCfg      config.SessionConfig
	GlobalRisk      *risk.GlobalRiskController
	StepPriceByTicker map[string]float64
}

type tickerState struct {
	cfg       RunnerConfig
	strat     strategy.CandleStrategy
	riskMgr   *risk.RiskManager
	position  *position.State
	tradesToday int
	eodCloseDate string
}

// PortfolioRunner симулирует несколько тикеров с общим глобальным риск-контроллером.
type PortfolioRunner struct {
	cfg      PortfolioRunnerConfig
	session  *engine.SessionClock
	states   map[string]*tickerState
	global   *risk.GlobalRiskController
	store    interfaces.TradeStore
	riskResetDate string
}

// NewPortfolioRunner создаёт портфельный симулятор.
func NewPortfolioRunner(cfg PortfolioRunnerConfig, store interfaces.TradeStore) (*PortfolioRunner, error) {
	if len(cfg.Tickers) == 0 {
		return nil, fmt.Errorf("simulation: portfolio tickers пуст")
	}
	if store == nil {
		store = interfaces.NoopTradeStore{}
	}

	clock, err := engine.NewSessionClock(
		cfg.SessionCfg.Timezone,
		cfg.SessionCfg.EODCloseTime,
		cfg.SessionCfg.SessionOpenTime,
		cfg.SessionCfg.EntryDelayMinutes,
	)
	if err != nil {
		return nil, err
	}

	states := make(map[string]*tickerState, len(cfg.Tickers))
	for ticker, rc := range cfg.Tickers {
		if rc.StepPriceValue <= 0 {
			rc.StepPriceValue = 1.0
		}
		if rc.Strategy == nil {
			return nil, fmt.Errorf("simulation: strategy не задана для %s", ticker)
		}
		trailCfg := rc.TrailCfg
		if trailCfg.StepPriceValue <= 0 {
			trailCfg.StepPriceValue = rc.StepPriceValue
		}
		rc.TrailCfg = trailCfg
		states[ticker] = &tickerState{
			cfg:     rc,
			strat:   rc.Strategy,
			riskMgr: risk.NewRiskManager(rc.Deposit, rc.MaxDailyLoss, rc.RiskPerTradePct, rc.StepPriceValue),
		}
	}

	return &PortfolioRunner{
		cfg:     cfg,
		session: clock,
		states:  states,
		global:  cfg.GlobalRisk,
		store:   store,
	}, nil
}

type candleEvent struct {
	ticker string
	candle models.Candle
}

// Run прогоняет свечи всех тикеров в хронологическом порядке.
func (p *PortfolioRunner) Run(ctx context.Context, candlesByTicker map[string][]models.Candle, executor interfaces.OrderExecutor) error {
	events := p.mergeCandles(candlesByTicker)
	for _, ev := range events {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		p.processEvent(ctx, executor, ev)
	}
	return nil
}

func (p *PortfolioRunner) mergeCandles(candlesByTicker map[string][]models.Candle) []candleEvent {
	var events []candleEvent
	for ticker, candles := range candlesByTicker {
		for _, c := range candles {
			candle := c
			if candle.Ticker == "" {
				candle.Ticker = ticker
			}
			events = append(events, candleEvent{ticker: ticker, candle: candle})
		}
	}
	sort.Slice(events, func(i, j int) bool {
		if events[i].candle.Timestamp.Equal(events[j].candle.Timestamp) {
			return events[i].ticker < events[j].ticker
		}
		return events[i].candle.Timestamp.Before(events[j].candle.Timestamp)
	})
	return events
}

func (p *PortfolioRunner) processEvent(ctx context.Context, executor interfaces.OrderExecutor, ev candleEvent) {
	st, ok := p.states[ev.ticker]
	if !ok {
		return
	}
	candle := ev.candle

	p.checkDailyReset(candle.Timestamp)
	p.checkEOD(ctx, executor, st, candle)

	if st.position != nil {
		p.processIntrabar(ctx, executor, st, candle)
	}
	if st.position == nil {
		p.processCandle(ctx, executor, st, candle)
	}
}

func (p *PortfolioRunner) processCandle(ctx context.Context, executor interfaces.OrderExecutor, st *tickerState, candle models.Candle) {
	if !p.session.EntriesAllowed(candle.Timestamp) {
		return
	}
	if st.cfg.MaxTradesPerDay > 0 && st.tradesToday >= st.cfg.MaxTradesPerDay {
		return
	}

	signal := st.strat.OnCandle(candle)
	if signal == nil {
		return
	}
	if err := st.riskMgr.CheckCircuitBreaker(); err != nil {
		return
	}
	if p.global != nil {
		if err := p.global.CanOpenPosition(); err != nil {
			return
		}
	}

	qty := st.riskMgr.CalculatePositionSize(signal.Price, signal.StopLoss)
	if qty <= 0 {
		return
	}
	signal.Quantity = qty
	signal.OrderType = models.OrderTypeLimit

	tradeRisk := abs(signal.Price-signal.StopLoss) * float64(qty) * st.cfg.StepPriceValue
	if p.global != nil {
		if err := p.global.PreTradeCheck(tradeRisk); err != nil {
			return
		}
	}

	if err := executor.ExecuteOrder(ctx, *signal); err != nil {
		return
	}

	st.tradesToday++
	st.position = position.NewFromSignal(*signal, candle.Timestamp)
	if p.global != nil {
		p.global.RegisterOpen(candle.Ticker, st.position.RDistance*float64(st.position.Quantity)*st.cfg.StepPriceValue)
	}
}

func (p *PortfolioRunner) processIntrabar(ctx context.Context, executor interfaces.OrderExecutor, st *tickerState, candle models.Candle) {
	for _, price := range position.IntrabarPrices(candle, st.position.Direction) {
		position.UpdateMFE(st.position, price)
		position.UpdateMAE(st.position, price)
		trailing.Apply(st.position, price, st.cfg.TrailCfg)
		if reason := position.CheckExit(st.position, price); reason != "" {
			p.closePosition(ctx, executor, st, price, reason, candle.Timestamp)
			return
		}
	}
}

func (p *PortfolioRunner) checkEOD(ctx context.Context, executor interfaces.OrderExecutor, st *tickerState, candle models.Candle) {
	ts := candle.Timestamp
	if !p.session.ShouldForceClose(ts) {
		if p.session.EntriesAllowed(ts) {
			st.eodCloseDate = ""
		}
		return
	}
	today := p.session.Today(ts)
	if st.eodCloseDate == today {
		return
	}
	if st.position != nil {
		p.closePosition(ctx, executor, st, candle.Close, models.CloseReasonEOD, ts)
	}
	st.eodCloseDate = today
}

func (p *PortfolioRunner) checkDailyReset(now time.Time) {
	if !p.session.IsSessionOpen(now) {
		return
	}
	today := p.session.Today(now)
	if p.riskResetDate == today {
		return
	}
	for _, st := range p.states {
		st.riskMgr.ResetDaily()
		st.tradesToday = 0
	}
	if p.global != nil {
		p.global.ResetDaily(today)
	}
	p.riskResetDate = today
}

func (p *PortfolioRunner) closePosition(ctx context.Context, executor interfaces.OrderExecutor, st *tickerState, price float64, reason string, closedAt time.Time) {
	if st.position == nil {
		return
	}

	pos := st.position
	st.position = nil

	closeDir := "SELL"
	if pos.Direction == "SELL" {
		closeDir = "BUY"
	}

	order := models.Order{
		Ticker:      st.cfg.Ticker,
		Direction:   closeDir,
		Quantity:    pos.Quantity,
		Price:       price,
		OrderType:   models.OrderTypeMarket,
		CloseReason: reason,
	}
	if err := executor.ExecuteOrder(ctx, order); err != nil {
		st.position = pos
		return
	}

	pnl := position.CalcPnL(pos, price, st.cfg.StepPriceValue)
	if pnl < 0 {
		st.riskMgr.RegisterLoss(-pnl)
	}
	if p.global != nil {
		p.global.RegisterClose(st.cfg.Ticker, pnl)
	}

	riskAmount := pos.RDistance * float64(pos.Quantity) * st.cfg.StepPriceValue
	pnlR := 0.0
	if riskAmount > 0 {
		pnlR = pnl / riskAmount
	}

	trade := models.ClosedTrade{
		TradingMode:       st.cfg.TradingMode,
		RunID:             st.cfg.RunID,
		ExperimentID:      st.cfg.ExperimentID,
		StopMode:          st.cfg.StopMode,
		Ticker:            st.cfg.Ticker,
		ClassCode:         st.cfg.ClassCode,
		StepPriceValue:    st.cfg.StepPriceValue,
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
		TradingDate:       p.session.Today(closedAt),
		CandleTimeframe:   st.cfg.CandleTimeframe,
		Lookback:             st.cfg.Lookback,
		RiskPerTradePct:      st.cfg.RiskPerTradePct,
		DepositPerTicker:     st.cfg.Deposit,
		StrategyParamsJSON:   st.cfg.StrategyParamsJSON,
	}
	_ = p.store.SaveClosedTrade(ctx, trade)
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}
