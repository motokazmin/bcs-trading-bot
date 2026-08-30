package engine

import (
	"context"
	"time"

	"bcs-trading-bot/internal/engine/contract"
	"bcs-trading-bot/internal/engine/risk"
	"bcs-trading-bot/internal/logx"
	"bcs-trading-bot/internal/models"
)

// strategyContext — конкретная реализация contract.StrategyContext,
// связывающая одну Strategy с каркасом: каналы данных (уже подписанные
// через internal/datafeed до старта), GlobalRiskController, OrderExecutor,
// TradeStore.
type strategyContext struct {
	ticker    string
	timeframe string
	candleCh  <-chan models.Candle
	tickCh    <-chan models.Tick
	executor  contract.OrderExecutor
	risk      *risk.GlobalRiskController
	store     contract.TradeStore
}

func (s *strategyContext) Ticker() string                 { return s.ticker }
func (s *strategyContext) Timeframe() string              { return s.timeframe }
func (s *strategyContext) Candles() <-chan models.Candle  { return s.candleCh }
func (s *strategyContext) Ticks() <-chan models.Tick      { return s.tickCh }
func (s *strategyContext) Orders() contract.OrderPort     { return s.executor }
func (s *strategyContext) Risk() contract.RiskPort        { return s.risk }
func (s *strategyContext) Trades() contract.TradeRecorder { return s.store }

// StrategyRunner запускает одну самодостаточную Strategy (ADR 0001) для
// одного тикера. StrategyRunner не знает ничего про SL/TP/трейлинг/EOD —
// вся эта логика внутри самой Strategy (SelfManagedStrategy).
type StrategyRunner struct {
	strategy   contract.Strategy
	sctx       *strategyContext
	globalRisk *risk.GlobalRiskController
	clock      *SessionClock
}

// NewStrategyRunner создаёт раннер. candleCh/tickCh — уже зарегистрированные
// каналы под datafeed.Feed.Subscribe(ticker, timeframe, candleCh, tickCh)
// (композиция происходит в cmd/bot/main.go, StrategyRunner сам ничего не
// подписывает — см. Решение 1, ADR 0001: DataFeed остаётся снаружи).
func NewStrategyRunner(
	strat contract.Strategy,
	ticker, timeframe string,
	candleCh <-chan models.Candle,
	tickCh <-chan models.Tick,
	executor contract.OrderExecutor,
	globalRisk *risk.GlobalRiskController,
	store contract.TradeStore,
	clock *SessionClock,
) *StrategyRunner {
	if store == nil {
		store = contract.NoopTradeStore{}
	}
	return &StrategyRunner{
		strategy:   strat,
		globalRisk: globalRisk,
		clock:      clock,
		sctx: &strategyContext{
			ticker:    ticker,
			timeframe: timeframe,
			candleCh:  candleCh,
			tickCh:    tickCh,
			executor:  executor,
			risk:      globalRisk,
			store:     store,
		},
	}
}

// Start блокируется до ctx.Done() — вызывать в отдельной горутине.
func (r *StrategyRunner) Start(ctx context.Context) {
	label := r.strategy.ID() + "/" + r.sctx.ticker
	logx.WorkerLifecycle(label, "strategy runner запущен")
	go r.globalDailyResetLoop(ctx)
	r.strategy.Run(ctx, r.sctx)
	logx.WorkerLifecycle(label, "strategy runner остановлен")
}

// globalDailyResetLoop сбрасывает дневные счётчики портфельного
// GlobalRiskController в начале каждой торговой сессии. Portfolio-риск —
// зона каркаса, а не стратегии: RiskPort намеренно не даёт стратегии
// ResetDaily, чтобы она не могла снять circuit breaker сама. ResetDaily
// идемпотентен по tradingDate, поэтому несколько раннеров на один
// контроллер безопасны.
func (r *StrategyRunner) globalDailyResetLoop(ctx context.Context) {
	if r.globalRisk == nil || r.clock == nil {
		return
	}
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			now := time.Now()
			if !r.clock.IsSessionOpen(now) {
				continue
			}
			r.globalRisk.ResetDaily(r.clock.Today(now))
		}
	}
}
