package engine

import (
	"context"
	"fmt"
	"math"
	"time"

	"bcs-trading-bot/internal/config"
	"bcs-trading-bot/internal/costs"
	"bcs-trading-bot/internal/position"
	"bcs-trading-bot/internal/risk"
	"bcs-trading-bot/internal/strategy"
	"bcs-trading-bot/internal/trailing"
	"bcs-trading-bot/pkg/interfaces"
	"bcs-trading-bot/pkg/logx"
	"bcs-trading-bot/pkg/models"
)

// TickerWorker инкапсулирует торговый цикл для одного тикера (в рамках одного эксперимента).
type TickerWorker struct {
	label            string
	ticker           string
	experimentID     string
	stopMode         string
	stepPriceValue   float64
	tradingMode      string
	runID            string
	classCode        string
	candleTimeframe  string
	lookback         int
	riskPerTradePct  float64
	depositPerTicker float64 // полный депозит счёта (имя колонки ClosedTrade / SQLite)
	strategy         strategy.CandleStrategy
	strategyID       string
	riskMgr          *risk.RiskManager
	globalRisk       *risk.GlobalRiskController
	session          *SessionClock
	store            interfaces.TradeStore
	trailCfg         trailing.Config
	costsCfg         costs.Config
	candleChan       chan models.Candle
	tickChan         chan models.Tick
	position         *position.State
	lastPrice        float64
	eodCloseDate             string
	riskResetDate            string
	tradesToday              int
	maxTradesPerTickerPerDay   int
}

// NewTickerWorker создаёт изолированный воркер для тикера.
func NewTickerWorker(
	ticker string,
	exp config.ResolvedExperiment,
	stepPriceValue float64,
	costsCfg costs.Config,
	sessionCfg config.SessionConfig,
	tradingMode, runID, classCode, candleTimeframe string,
	store interfaces.TradeStore,
	globalRisk *risk.GlobalRiskController,
) (*TickerWorker, error) {
	clock, err := NewSessionClockExt(
		sessionCfg.Timezone,
		sessionCfg.EODCloseTime,
		sessionCfg.SessionOpenTime,
		sessionCfg.EntryDelayMinutes,
		sessionCfg.WeekdaysOnly,
		sessionCfg.WeekendOnly,
	)
	if err != nil {
		return nil, err
	}

	if stepPriceValue <= 0 {
		stepPriceValue = 1.0
	}
	if store == nil {
		store = interfaces.NoopTradeStore{}
	}

	deposit := exp.Risk.Deposit
	maxLoss := exp.Risk.MaxDailyLoss
	label := ticker
	if exp.ID != "" && exp.ID != "default" {
		label = fmt.Sprintf("%s/%s", exp.ID, ticker)
	}

	trailCfg := exp.Strategy.TrailingConfig(stepPriceValue, costsCfg, classCode)

	strat, err := exp.Strategy.BuildStrategy(sessionCfg)
	if err != nil {
		return nil, fmt.Errorf("стратегия: %w", err)
	}

	return &TickerWorker{
		label:                    label,
		ticker:                   ticker,
		experimentID:             exp.ID,
		stopMode:                 exp.Strategy.StopMode,
		stepPriceValue:           stepPriceValue,
		tradingMode:              tradingMode,
		runID:                    runID,
		classCode:                classCode,
		candleTimeframe:          candleTimeframe,
		lookback:                 exp.Strategy.Lookback,
		riskPerTradePct:          exp.Risk.RiskPerTradePercent,
		depositPerTicker:         deposit,
		strategy:                 strat,
		strategyID:               exp.Strategy.TypeOrDefault(),
		riskMgr:                  risk.NewRiskManager(deposit, maxLoss, exp.Risk.RiskPerTradePercent, stepPriceValue),
		globalRisk:               globalRisk,
		session:                  clock,
		store:                    store,
		trailCfg:                 trailCfg,
		costsCfg:                 costsCfg,
		maxTradesPerTickerPerDay: exp.Strategy.MaxTradesPerTickerPerDay,
		candleChan:               make(chan models.Candle, 64),
		tickChan:                 make(chan models.Tick, 256),
	}, nil
}

func (w *TickerWorker) CandleChan() chan<- models.Candle { return w.candleChan }
func (w *TickerWorker) TickChan() chan<- models.Tick     { return w.tickChan }
func (w *TickerWorker) Ticker() string                   { return w.ticker }
func (w *TickerWorker) ExperimentID() string             { return w.experimentID }
func (w *TickerWorker) Label() string                    { return w.label }

// Start запускает цикл обработки тиков, свечей и контроля позиции.
func (w *TickerWorker) Start(ctx context.Context, executor interfaces.OrderExecutor) {
	logx.WorkerLifecycle(w.label, "воркер запущен")

	eodTicker := time.NewTicker(30 * time.Second)
	defer eodTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			logx.WorkerLifecycle(w.label, "воркер остановлен")
			return

		case tick, ok := <-w.tickChan:
			if !ok {
				logx.WorkerLifecycle(w.label, "канал тиков закрыт")
				return
			}
			w.lastPrice = tick.Price
			w.checkDailyReset(time.Now())
			w.checkSLTP(ctx, executor, tick.Price)
			w.checkEOD(ctx, executor, tick.Price, time.Now())

		case candle, ok := <-w.candleChan:
			if !ok {
				logx.WorkerLifecycle(w.label, "канал свечей закрыт")
				return
			}
			w.lastPrice = candle.Close
			w.checkDailyReset(candle.Timestamp)
			w.checkEOD(ctx, executor, candle.Close, candle.Timestamp)
			w.processCandle(ctx, executor, candle)

		case <-eodTicker.C:
			now := time.Now()
			w.checkDailyReset(now)
			if w.lastPrice > 0 {
				w.checkEOD(ctx, executor, w.lastPrice, now)
			}
		}
	}
}

func (w *TickerWorker) processCandle(ctx context.Context, executor interfaces.OrderExecutor, candle models.Candle) {
	if ctx.Err() != nil {
		return
	}

	if w.position != nil {
		return
	}

	if !w.session.EntriesAllowed(candle.Timestamp) {
		return
	}

	if w.maxTradesPerTickerPerDay > 0 && w.tradesToday >= w.maxTradesPerTickerPerDay {
		return
	}

	signal := w.strategy.OnCandle(candle)
	if signal == nil {
		return
	}

	if err := w.riskMgr.CheckCircuitBreaker(); err != nil {
		logx.SignalRejected(w.label, signal.Direction, err.Error())
		return
	}

	if w.globalRisk != nil {
		if err := w.globalRisk.CanOpenPosition(); err != nil {
			logx.SignalRejected(w.label, signal.Direction, err.Error())
			return
		}
		if err := w.globalRisk.CanOpenTicker(w.ticker); err != nil {
			logx.SignalRejected(w.label, signal.Direction, err.Error())
			return
		}
	}

	quantity := w.riskMgr.CalculatePositionSize(signal.Price, signal.StopLoss)
	if quantity <= 0 {
		logx.SignalRejected(w.label, signal.Direction, "нулевой объём позиции")
		return
	}

	// BUY: risk-sizing не знает свободный кэш — режем notional до GetBalance.
	if signal.Direction == "BUY" {
		if bal, err := executor.GetBalance(ctx); err == nil {
			quantity = risk.CapQuantityByCash(quantity, signal.Price, bal)
			if quantity <= 0 {
				logx.SignalRejected(w.label, signal.Direction, "недостаточно средств")
				return
			}
		}
	}

	tradeRisk := math.Abs(signal.Price-signal.StopLoss) * float64(quantity) * w.stepPriceValue
	if w.globalRisk != nil {
		if err := w.globalRisk.PreTradeCheck(tradeRisk); err != nil {
			logx.SignalRejected(w.label, signal.Direction, err.Error())
			return
		}
	}

	signal.Quantity = quantity
	signal.OrderType = models.OrderTypeLimit

	if err := executor.ExecuteOrder(ctx, *signal); err != nil {
		logx.Error("[%s] ошибка исполнения ордера: %v", w.label, err)
		return
	}

	logx.TradeOpen(w.label, signal.Direction, signal.Quantity, signal.Price, signal.StopLoss, signal.TakeProfit)
	w.tradesToday++
	w.position = position.NewFromSignal(*signal, candle.Timestamp)
	if w.globalRisk != nil {
		tradeRisk := w.position.RDistance * float64(w.position.Quantity) * w.stepPriceValue
		w.globalRisk.RegisterOpen(w.ticker, tradeRisk)
	}
}

func (w *TickerWorker) checkSLTP(ctx context.Context, executor interfaces.OrderExecutor, price float64) {
	if w.position == nil {
		return
	}

	position.UpdateMFE(w.position, price)
	position.UpdateMAE(w.position, price)
	w.updateTrailingStop(price)

	if reason := position.CheckExit(w.position, price); reason != "" {
		w.closePosition(ctx, executor, price, reason)
	}
}

func (w *TickerWorker) updateTrailingStop(price float64) {
	if w.position == nil {
		return
	}
	prevStage := w.position.TrailStage
	trailing.Apply(w.position, price, w.trailCfg)
	if w.position.TrailStage > prevStage {
		logx.Trailing(w.label, w.position.TrailStage, w.position.StopLoss)
	}
}

func (w *TickerWorker) checkEOD(ctx context.Context, executor interfaces.OrderExecutor, price float64, now time.Time) {
	if !w.session.ShouldForceClose(now) {
		if w.session.EntriesAllowed(now) {
			w.eodCloseDate = ""
		}
		return
	}

	today := w.session.today(now)
	if w.eodCloseDate == today {
		return
	}

	if w.position != nil {
		w.closePosition(ctx, executor, price, models.CloseReasonEOD)
	}

	w.eodCloseDate = today
}

func (w *TickerWorker) checkDailyReset(now time.Time) {
	if !w.session.IsSessionOpen(now) {
		return
	}

	today := w.session.today(now)
	if w.riskResetDate == today {
		return
	}

	w.riskMgr.ResetDaily()
	w.tradesToday = 0
	w.riskResetDate = today
	if w.globalRisk != nil {
		w.globalRisk.ResetDaily(today)
	}
	logx.DailyReset(w.label)
}

// closePosition закрывает позицию; вызывать только из горутины Start.
func (w *TickerWorker) closePosition(ctx context.Context, executor interfaces.OrderExecutor, price float64, reason string) {
	if w.position == nil {
		return
	}

	pos := w.position
	w.position = nil

	closeDir := "SELL"
	if pos.Direction == "SELL" {
		closeDir = "BUY"
	}

	order := models.Order{
		Ticker:        w.ticker,
		Direction:     closeDir,
		Quantity:      pos.Quantity,
		Price:         price,
		OrderType:     models.OrderTypeMarket,
		CloseReason:   reason,
		CommissionRub: 0,
	}

	grossPnL := position.CalcPnL(pos, price, w.stepPriceValue)
	commission := costs.RoundTrip(w.costsCfg, w.classCode, pos.EntryPrice, price, pos.Quantity, w.stepPriceValue)
	order.CommissionRub = commission
	pnl := costs.NetPnL(grossPnL, w.costsCfg, w.classCode, pos.EntryPrice, price, pos.Quantity, w.stepPriceValue)

	if err := executor.ExecuteOrder(ctx, order); err != nil {
		logx.Error("[%s] ошибка закрытия позиции (%s): %v", w.label, reason, err)
		w.position = pos
		return
	}

	if pnl < 0 {
		w.riskMgr.RegisterLoss(-pnl)
	} else if pnl > 0 {
		w.riskMgr.RegisterProfit(pnl)
	}
	if w.globalRisk != nil {
		w.globalRisk.RegisterClose(w.ticker, pnl)
	}

	closedAt := time.Now()
	riskAmount := pos.RDistance * float64(pos.Quantity) * w.stepPriceValue
	pnlR := 0.0
	if riskAmount > 0 {
		pnlR = pnl / riskAmount
	}

	trade := models.ClosedTrade{
		TradingMode:       w.tradingMode,
		RunID:             w.runID,
		ExperimentID:      w.experimentID,
		StopMode:          w.stopMode,
		Ticker:            w.ticker,
		ClassCode:         w.classCode,
		StepPriceValue:    w.stepPriceValue,
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
		TradingDate:       w.session.today(closedAt),
		CandleTimeframe:   w.candleTimeframe,
		Lookback:          w.lookback,
		RiskPerTradePct:   w.riskPerTradePct,
		DepositPerTicker:  w.depositPerTicker,
	}
	if err := w.store.SaveClosedTrade(context.Background(), trade); err != nil {
		logx.Error("[%s] ошибка сохранения сделки в БД: %v", w.label, err)
	}

	logx.TradeClose(w.label, reason, price, pnl, pnlR)
}
