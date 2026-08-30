// Package selfmanaged реализует SelfManagedStrategy (ADR 0001).
// Оборачивает любой strategy.CandleStrategy (сигнальный "мозг") в
// самодостаточную contract.Strategy: сама ведёт позицию, SL/TP/трейлинг,
// EOD-закрытие, сайзинг и экспорт закрытой сделки.
//
// Что делает адаптер поверх сигнала:
//   - stale-guard входа (не входить по устаревшему бару после реконнекта WS);
//   - tradeaudit ValidateOpen/ValidateClose → audit_* в ClosedTrade;
//   - ghost-handling: ErrNoOpenPosition → дроп позиции, прочие ошибки
//     исполнителя на закрытии → восстановление позиции для повтора;
//   - снапшот позиции для live-дашборда (contract.PositionSource → live.Hub).
package selfmanaged

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
	"time"

	"bcs-trading-bot/internal/engine/costs"
	"bcs-trading-bot/internal/engine"
	"bcs-trading-bot/internal/engine/contract"
	"bcs-trading-bot/internal/logx"
	"bcs-trading-bot/internal/models"
	"bcs-trading-bot/internal/engine/position"
	"bcs-trading-bot/internal/engine/risk"
	"bcs-trading-bot/internal/strategy"
	"bcs-trading-bot/internal/engine/tradeaudit"
	"bcs-trading-bot/internal/engine/trailing"
)

var _ contract.PositionSource = (*SelfManagedStrategy)(nil)

// SessionClock — то подмножество engine.SessionClock, которое нужно
// SelfManagedStrategy. Узкий интерфейс на стороне потребителя (а не импорт
// конкретного *engine.SessionClock) держит контракт минимальным и облегчает
// тесты. Реальная реализация (*engine.SessionClock) передаётся из
// internal/app и удовлетворяет этому интерфейсу структурно.
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
	Label           string                  // для логов, напр. "strategy/or-fade-conservative/LKOH"
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
// сигнального генератора. Реализует contract.Strategy и
// contract.PositionSource.
type SelfManagedStrategy struct {
	cfg     Config
	riskMgr *risk.RiskManager

	mu        sync.RWMutex // защищает pos + lastPrice (SnapshotPosition из HTTP-горутины)
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

func (s *SelfManagedStrategy) ID() string           { return s.cfg.Signal.ID() }
func (s *SelfManagedStrategy) Label() string        { return s.cfg.Label }
func (s *SelfManagedStrategy) Ticker() string       { return s.cfg.Ticker }
func (s *SelfManagedStrategy) ExperimentID() string { return s.cfg.ExperimentID }

// SnapshotPosition возвращает копию открытой позиции (или nil) для live-дашборда.
// Вызывается из HTTP-горутины live.Hub — доступ под RLock.
func (s *SelfManagedStrategy) SnapshotPosition() *models.PositionSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.pos == nil {
		return nil
	}
	pos := *s.pos
	last := s.lastPrice
	step := s.cfg.StepPriceValue
	if step <= 0 {
		step = 1
	}
	return &models.PositionSnapshot{
		ID:            fmt.Sprintf("%s/%s", s.cfg.ExperimentID, s.cfg.Ticker),
		ExperimentID:  s.cfg.ExperimentID,
		Ticker:        s.cfg.Ticker,
		Direction:     pos.Direction,
		Quantity:      pos.Quantity,
		EntryPrice:    pos.EntryPrice,
		StopLoss:      pos.StopLoss,
		TakeProfit:    pos.TakeProfit,
		TrailStage:    pos.TrailStage,
		OpenedAt:      pos.OpenedAt,
		LastPrice:     last,
		UnrealizedPnL: position.CalcPnL(&pos, last, step),
		RDistance:     pos.RDistance,
		StepPrice:     step,
	}
}

func (s *SelfManagedStrategy) hasPos() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.pos != nil
}

func (s *SelfManagedStrategy) setLastPrice(p float64) {
	s.mu.Lock()
	s.lastPrice = p
	s.mu.Unlock()
}

// Run — основной цикл. Блокируется до ctx.Done() или закрытия каналов
// StrategyContext.
func (s *SelfManagedStrategy) Run(ctx context.Context, sctx contract.StrategyContext) {
	logx.WorkerLifecycle(s.cfg.Label, "strategy запущена")
	defer logx.WorkerLifecycle(s.cfg.Label, "strategy остановлена")

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
			s.setLastPrice(tick.Price)
			now := time.Now()
			s.checkDailyReset(now)
			s.checkSLTP(ctx, sctx, tick.Price)
			s.checkEOD(ctx, sctx, tick.Price, now)

		case candle, ok := <-sctx.Candles():
			if !ok {
				return
			}
			now := time.Now()
			s.setLastPrice(candle.Close)
			s.checkDailyReset(now)
			s.checkEOD(ctx, sctx, candle.Close, now)
			s.processCandle(ctx, sctx, candle, now)

		case <-eodTicker.C:
			now := time.Now()
			s.checkDailyReset(now)
			s.mu.RLock()
			last := s.lastPrice
			s.mu.RUnlock()
			if last > 0 {
				s.checkEOD(ctx, sctx, last, now)
			}
		}
	}
}

func (s *SelfManagedStrategy) processCandle(ctx context.Context, sctx contract.StrategyContext, candle models.Candle, now time.Time) {
	if ctx.Err() != nil || s.hasPos() {
		return
	}

	// Stale-guard: не входить по переигранному/бэкфилл-бару после реконнекта WS
	// (порог 3×TF, см. engine.CandleFreshForEntry).
	if !engine.CandleFreshForEntry(now, candle.Timestamp, s.cfg.CandleTimeframe) {
		age := now.Sub(candle.Timestamp)
		logx.Warn("[%s] пропуск stale-свечи: bar=%s age=%s",
			s.cfg.Label, candle.Timestamp.Format(time.RFC3339), age.Round(time.Second))
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

	pos := position.NewFromSignal(*signal, now)
	pos.EntryBarTime = candle.Timestamp
	pos.EntryBarClose = candle.Close
	s.mu.Lock()
	s.pos = pos
	s.mu.Unlock()
	s.tradesToday++

	logx.TradeOpen(s.cfg.Label, signal.Direction, signal.Quantity, signal.Price, signal.StopLoss, signal.TakeProfit)
	logx.Info("[%s] bar_age=%s bar_time=%s",
		s.cfg.Label, now.Sub(candle.Timestamp).Round(time.Second), candle.Timestamp.Format(time.RFC3339))

	openAudit := tradeaudit.ValidateOpen(tradeaudit.OpenInput{
		Direction:   signal.Direction,
		EntryPrice:  signal.Price,
		StopLoss:    signal.StopLoss,
		TakeProfit:  signal.TakeProfit,
		RDistance:   pos.RDistance,
		BarClose:    candle.Close,
		LastPrice:   s.currentLastPrice(),
		RewardRatio: s.cfg.RewardRatio,
	})
	if !openAudit.Empty() {
		logx.Audit(s.cfg.Label, openAudit.Severity, openAudit.CodesCSV(), openAudit.DetailsString())
	}

	// Limit-fill на баре: если на том же баре уже пробит SL/TP — закрыть по
	// уровню, не ждать adverse tick.
	if reason := position.SameBarExitAfterFill(pos, candle); reason != "" {
		exitPx := position.ExitFillPrice(pos, reason, candle.Close)
		s.mu.Lock()
		if s.pos != nil {
			s.pos.SameBarExit = true
			position.UpdateMAE(s.pos, exitPx)
			position.UpdateMFE(s.pos, exitPx)
		}
		s.mu.Unlock()
		logx.Info("[%s] same-bar exit after fill: %s @ %.4f (bar close %.4f)", s.cfg.Label, reason, exitPx, candle.Close)
		s.closePosition(ctx, sctx, exitPx, reason)
		return
	}
	if last := s.currentLastPrice(); last > 0 {
		s.checkSLTP(ctx, sctx, last)
	}
}

func (s *SelfManagedStrategy) currentLastPrice() float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lastPrice
}

func (s *SelfManagedStrategy) checkSLTP(ctx context.Context, sctx contract.StrategyContext, price float64) {
	s.mu.Lock()
	if s.pos == nil {
		s.mu.Unlock()
		return
	}
	position.UpdateMFE(s.pos, price)
	position.UpdateMAE(s.pos, price)
	prevStage := s.pos.TrailStage
	trailing.Apply(s.pos, price, s.cfg.TrailCfg)
	stage := s.pos.TrailStage
	sl := s.pos.StopLoss
	reason := position.CheckExit(s.pos, price)
	exitPx := price
	if reason != "" {
		exitPx = position.ExitFillPrice(s.pos, reason, price)
	}
	s.mu.Unlock()

	if stage > prevStage {
		logx.Trailing(s.cfg.Label, stage, sl)
	}
	if reason != "" {
		s.closePosition(ctx, sctx, exitPx, reason)
	}
}

func (s *SelfManagedStrategy) checkEOD(ctx context.Context, sctx contract.StrategyContext, price float64, now time.Time) {
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
	if s.hasPos() {
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

func (s *SelfManagedStrategy) closePosition(ctx context.Context, sctx contract.StrategyContext, price float64, reason string) {
	s.mu.Lock()
	if s.pos == nil {
		s.mu.Unlock()
		return
	}
	pos := s.pos
	s.pos = nil
	s.mu.Unlock()

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
		if errors.Is(err, contract.ErrNoOpenPosition) {
			// Ghost: локальная позиция есть, у исполнителя уже нет — не
			// восстанавливаем (иначе spam повторных закрытий), только
			// освобождаем риск.
			sctx.Risk().ReleaseOpen(s.cfg.Ticker)
			return
		}
		// Транзиентная ошибка исполнителя — возвращаем позицию, риск не
		// трогаем (резерв всё ещё соответствует открытой позиции). Следующий
		// тик/свеча повторит попытку выхода.
		s.mu.Lock()
		s.pos = pos
		s.mu.Unlock()
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

	audit := tradeaudit.AnnotateTrade(&trade, tradeaudit.OpenInput{
		Direction:   pos.Direction,
		EntryPrice:  pos.EntryPrice,
		StopLoss:    pos.InitialStopLoss,
		TakeProfit:  pos.InitialTakeProfit,
		RDistance:   pos.RDistance,
		BarClose:    pos.EntryBarClose,
		RewardRatio: s.cfg.RewardRatio,
	}, tradeaudit.CloseInput{
		Direction:       pos.Direction,
		EntryPrice:      pos.EntryPrice,
		ExitPrice:       price,
		FinalStopLoss:   pos.StopLoss,
		TakeProfit:      pos.InitialTakeProfit,
		RDistance:       pos.RDistance,
		CloseReason:     reason,
		PnLR:            pnlR,
		MFEinR:          trade.MFEinR,
		TrailStage:      pos.TrailStage,
		TrailActivation: s.cfg.TrailCfg.ActivationR,
		HoldSeconds:     trade.HoldSeconds,
		BarDuration:     engine.CandleBarDuration(s.cfg.CandleTimeframe),
		SameBarExit:     pos.SameBarExit,
		EntryBarClose:   pos.EntryBarClose,
	})
	if !audit.Empty() {
		logx.Audit(s.cfg.Label, audit.Severity, audit.CodesCSV(), audit.DetailsString())
	}

	if err := sctx.Trades().SaveClosedTrade(context.Background(), trade); err != nil {
		logx.Error("[%s] ошибка сохранения сделки в БД: %v", s.cfg.Label, err)
	}

	logx.TradeClose(s.cfg.Label, reason, price, pnl, pnlR)
}
