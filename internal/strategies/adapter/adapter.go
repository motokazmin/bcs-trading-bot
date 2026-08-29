// Package adapter реализует SelfManagedStrategy (ADR 0001, Фазы 3–5).
// Оборачивает любой существующий strategy.CandleStrategy (сигнальный "мозг")
// в самодостаточную strategy.Strategy: сама ведёт позицию, SL/TP/трейлинг,
// EOD-закрытие, сайзинг и экспорт закрытой сделки.
//
// Известные дыры, пока не закрытые (задокументированы в portfolio-paper.yaml):
//   - live-дашборд (internal/live.Hub) не получает снапшоты позиций;
//   - tradeaudit ValidateOpen/ValidateClose не вызываются;
//   - ghost-position handling упрощён.
package adapter

import (
	"context"
	"math"
	"time"

	"bcs-trading-bot/internal/costs"
	"bcs-trading-bot/internal/position"
	"bcs-trading-bot/internal/risk"
	"bcs-trading-bot/internal/strategy"
	"bcs-trading-bot/internal/trailing"
	"bcs-trading-bot/pkg/logx"
	"bcs-trading-bot/pkg/models"
)

// SessionClock — то подмножество internal/engine.SessionClock, которое
// нужно адаптеру. Отдельный интерфейс здесь (а не прямой импорт
// internal/engine) — потому что internal/engine импортирует internal/strategy
// (за Strategy/CandleStrategy), и обратный импорт создал бы цикл. Реальная
// реализация (*engine.SessionClock) передаётся из cmd/bot/main.go — она уже
// удовлетворяет этому интерфейсу, ничего дублировать не нужно.
type SessionClock interface {
	EntriesAllowed(now time.Time) bool
	ShouldForceClose(now time.Time) bool
	IsSessionOpen(now time.Time) bool
	Today(now time.Time) string
}

// Config — всё, что нужно SelfManagedStrategy для одного (тикер, эксперимент).
// Собирается вызывающей стороной (cmd/bot/main.go).
type Config struct {
	Signal          strategy.CandleStrategy // сигнальный "мозг" (существующий OnCandle)
	Label           string                  // для логов, напр. "pilot/midday_compression/LKOH"
	Ticker          string
	ExperimentID    string
	StopMode        string
	StepPriceValue  float64
	TradingMode     string
	RunID           string
	ClassCode       string
	CandleTimeframe string
	Lookback        int
	RiskPerTradePct float64
	Deposit         float64
	MaxDailyLoss    float64
	TrailCfg        trailing.Config
	CostsCfg        costs.Config
	RewardRatio     float64
	MaxTradesPerDay int
	Session         SessionClock
}

// SelfManagedStrategy — самодостаточная стратегия поверх существующего
// сигнального генератора. Реализует strategy.Strategy.
type SelfManagedStrategy struct {
	cfg     Config
	riskMgr *risk.RiskManager

	pos       *position.State
	lastPrice float64

	eodCloseDate  string
	riskResetDate string
	tradesToday   int
}

// New создаёт стратегию. cfg.Signal — уже сконструированный CandleStrategy
// (exp.Strategy.BuildStrategy(sessionCfg)); сигнальная логика не переписывается.
func New(cfg Config) *SelfManagedStrategy {
	if cfg.StepPriceValue <= 0 {
		cfg.StepPriceValue = 1.0
	}
	return &SelfManagedStrategy{
		cfg:     cfg,
		riskMgr: risk.NewRiskManager(cfg.Deposit, cfg.MaxDailyLoss, cfg.RiskPerTradePct, cfg.StepPriceValue),
	}
}

func (s *SelfManagedStrategy) ID() string { return s.cfg.Signal.ID() }

// Run — основной цикл. Блокируется до ctx.Done() или закрытия каналов
// StrategyContext.
func (s *SelfManagedStrategy) Run(ctx context.Context, sctx strategy.StrategyContext) {
	logx.WorkerLifecycle(s.cfg.Label, "pilot strategy запущена")
	defer logx.WorkerLifecycle(s.cfg.Label, "pilot strategy остановлена")

	eodTicker := time.NewTicker(30 * time.Second)
	defer eodTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return

		case tick, ok := <-sctx.Ticks():
			if !ok {
				return
			}
			s.lastPrice = tick.Price
			now := time.Now()
			s.checkDailyReset(now)
			s.checkSLTP(ctx, sctx, tick.Price)
			s.checkEOD(ctx, sctx, tick.Price, now)

		case candle, ok := <-sctx.Candles():
			if !ok {
				return
			}
			now := time.Now()
			s.lastPrice = candle.Close
			s.checkDailyReset(now)
			s.checkEOD(ctx, sctx, candle.Close, now)
			s.processCandle(ctx, sctx, candle, now)

		case <-eodTicker.C:
			now := time.Now()
			s.checkDailyReset(now)
			if s.lastPrice > 0 {
				s.checkEOD(ctx, sctx, s.lastPrice, now)
			}
		}
	}
}

func (s *SelfManagedStrategy) processCandle(ctx context.Context, sctx strategy.StrategyContext, candle models.Candle, now time.Time) {
	if ctx.Err() != nil || s.pos != nil {
		return
	}
	if !s.cfg.Session.EntriesAllowed(now) {
		return
	}
	if s.cfg.MaxTradesPerDay > 0 && s.tradesToday >= s.cfg.MaxTradesPerDay {
		return
	}

	signal := s.cfg.Signal.OnCandle(candle)
	if signal == nil {
		return
	}

	if err := s.riskMgr.CheckCircuitBreaker(); err != nil {
		logx.SignalRejected(s.cfg.Label, signal.Direction, err.Error())
		return
	}

	quantity := s.riskMgr.CalculatePositionSize(signal.Price, signal.StopLoss)
	if quantity <= 0 {
		logx.SignalRejected(s.cfg.Label, signal.Direction, "нулевой объём позиции")
		return
	}
	if bal, err := sctx.Orders().GetBalance(ctx); err == nil {
		quantity = risk.CapQuantityByCash(quantity, signal.Price, bal, s.cfg.StepPriceValue)
		if quantity <= 0 {
			logx.SignalRejected(s.cfg.Label, signal.Direction, "недостаточно средств")
			return
		}
	}

	tradeRisk := math.Abs(signal.Price-signal.StopLoss) * float64(quantity) * s.cfg.StepPriceValue
	if err := sctx.Risk().TryOpen(s.cfg.Ticker, tradeRisk); err != nil {
		logx.SignalRejected(s.cfg.Label, signal.Direction, err.Error())
		return
	}

	signal.Quantity = quantity
	signal.OrderType = models.OrderTypeLimit
	signal.Ticker = s.cfg.Ticker

	if err := sctx.Orders().ExecuteOrder(ctx, *signal); err != nil {
		logx.Error("[%s] ошибка открытия позиции: %v", s.cfg.Label, err)
		sctx.Risk().ReleaseOpen(s.cfg.Ticker)
		return
	}

	s.pos = position.NewFromSignal(*signal, now)
	s.tradesToday++

	// Limit-fill на баре: если на том же баре уже пробит SL/TP — закрыть по
	// уровню, не ждать adverse tick.
	if reason := position.SameBarExitAfterFill(s.pos, candle); reason != "" {
		exitPx := position.ExitFillPrice(s.pos, reason, candle.Close)
		s.pos.SameBarExit = true
		position.UpdateMAE(s.pos, exitPx)
		position.UpdateMFE(s.pos, exitPx)
		logx.Info("[%s] same-bar exit after fill: %s @ %.4f (bar close %.4f)", s.cfg.Label, reason, exitPx, candle.Close)
		s.closePosition(ctx, sctx, exitPx, reason)
		return
	}
	if s.lastPrice > 0 {
		s.checkSLTP(ctx, sctx, s.lastPrice)
	}
}

func (s *SelfManagedStrategy) checkSLTP(ctx context.Context, sctx strategy.StrategyContext, price float64) {
	if s.pos == nil {
		return
	}
	position.UpdateMFE(s.pos, price)
	position.UpdateMAE(s.pos, price)
	prevStage := s.pos.TrailStage
	trailing.Apply(s.pos, price, s.cfg.TrailCfg)
	if s.pos.TrailStage > prevStage {
		logx.Trailing(s.cfg.Label, s.pos.TrailStage, s.pos.StopLoss)
	}
	reason := position.CheckExit(s.pos, price)
	if reason == "" {
		return
	}
	exitPx := position.ExitFillPrice(s.pos, reason, price)
	s.closePosition(ctx, sctx, exitPx, reason)
}

func (s *SelfManagedStrategy) checkEOD(ctx context.Context, sctx strategy.StrategyContext, price float64, now time.Time) {
	if !s.cfg.Session.ShouldForceClose(now) {
		if s.cfg.Session.EntriesAllowed(now) {
			s.eodCloseDate = ""
		}
		return
	}
	today := s.cfg.Session.Today(now)
	if s.eodCloseDate == today {
		return
	}
	if s.pos != nil {
		s.closePosition(ctx, sctx, price, models.CloseReasonEOD)
	}
	s.eodCloseDate = today
}

func (s *SelfManagedStrategy) checkDailyReset(now time.Time) {
	if !s.cfg.Session.IsSessionOpen(now) {
		return
	}
	today := s.cfg.Session.Today(now)
	if s.riskResetDate == today {
		return
	}
	s.riskMgr.ResetDaily()
	s.tradesToday = 0
	s.riskResetDate = today
	logx.DailyReset(s.cfg.Label)
}

func (s *SelfManagedStrategy) closePosition(ctx context.Context, sctx strategy.StrategyContext, price float64, reason string) {
	pos := s.pos
	if pos == nil {
		return
	}
	s.pos = nil

	price = position.ExitFillPrice(pos, reason, price)

	closeDir := "SELL"
	if pos.Direction == "SELL" {
		closeDir = "BUY"
	}

	order := models.Order{
		Ticker:      s.cfg.Ticker,
		Direction:   closeDir,
		Quantity:    pos.Quantity,
		Price:       price,
		OrderType:   models.OrderTypeMarket,
		CloseReason: reason,
	}

	grossPnL := position.CalcPnL(pos, price, s.cfg.StepPriceValue)
	commission := costs.RoundTrip(s.cfg.CostsCfg, s.cfg.ClassCode, pos.EntryPrice, price, pos.Quantity, s.cfg.StepPriceValue)
	order.CommissionRub = commission
	pnl := costs.NetPnL(grossPnL, s.cfg.CostsCfg, s.cfg.ClassCode, pos.EntryPrice, price, pos.Quantity, s.cfg.StepPriceValue)

	if err := sctx.Orders().ExecuteOrder(ctx, order); err != nil {
		logx.Error("[%s] ошибка закрытия позиции (%s): %v", s.cfg.Label, reason, err)
		// Ghost-позиция (у исполнителя её уже нет) — не восстанавливаем,
		// иначе спам повторных попыток закрыть несуществующее. Минимальная
		// версия просто освобождает риск (безопаснее, не откроет двойной
		// риск), но может потерять сделку при кратковременном сбое
		// исполнителя. Это известная дыра адаптера (см. portfolio-paper.yaml).
		sctx.Risk().ReleaseOpen(s.cfg.Ticker)
		return
	}

	if pnl < 0 {
		s.riskMgr.RegisterLoss(-pnl)
	} else if pnl > 0 {
		s.riskMgr.RegisterProfit(pnl)
	}
	sctx.Risk().RegisterClose(s.cfg.Ticker, pnl)

	closedAt := time.Now().UTC()
	riskAmount := pos.RDistance * float64(pos.Quantity) * s.cfg.StepPriceValue
	pnlR := 0.0
	if riskAmount > 0 {
		pnlR = pnl / riskAmount
	}

	trade := models.ClosedTrade{
		TradingMode:       s.cfg.TradingMode,
		RunID:             s.cfg.RunID,
		ExperimentID:      s.cfg.ExperimentID,
		StopMode:          s.cfg.StopMode,
		Ticker:            s.cfg.Ticker,
		ClassCode:         s.cfg.ClassCode,
		StepPriceValue:    s.cfg.StepPriceValue,
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
		TradingDate:       s.cfg.Session.Today(closedAt),
		CandleTimeframe:   s.cfg.CandleTimeframe,
		Lookback:          s.cfg.Lookback,
		RiskPerTradePct:   s.cfg.RiskPerTradePct,
		DepositPerTicker:  s.cfg.Deposit,
		EntryBarClose:     pos.EntryBarClose,
	}
	if !pos.EntryBarTime.IsZero() {
		trade.EntryBarTime = pos.EntryBarTime.UTC().Format(time.RFC3339)
	}

	if err := sctx.Trades().SaveClosedTrade(context.Background(), trade); err != nil {
		logx.Error("[%s] ошибка сохранения сделки в БД: %v", s.cfg.Label, err)
	}

	logx.TradeClose(s.cfg.Label, reason, price, pnl, pnlR)
}
