package engine

import (
	"context"
	"fmt"
	"math"
	"time"

	"bcs-trading-bot/internal/config"
	"bcs-trading-bot/internal/risk"
	"bcs-trading-bot/internal/strategy"
	"bcs-trading-bot/pkg/interfaces"
	"bcs-trading-bot/pkg/logx"
	"bcs-trading-bot/pkg/models"
)

const virtualCommissionPerLot = 5.0

type openPosition struct {
	direction         string
	quantity          int
	entryPrice        float64
	initialStopLoss   float64
	initialTakeProfit float64
	stopLoss          float64
	takeProfit        float64
	rDistance         float64
	trailStage        int
	mfePrice          float64
	maePrice          float64
	breakoutUpper     float64
	breakoutLower     float64
	openedAt          time.Time
}

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
	depositPerTicker float64
	strategy         *strategy.MomentumBreakout
	riskMgr          *risk.RiskManager
	session          *SessionClock
	store            interfaces.TradeStore
	candleChan       chan models.Candle
	tickChan         chan models.Tick
	position         *openPosition // только из горутины Start; mutex не нужен
	lastPrice        float64
	eodCloseDate              string
	riskResetDate             string
	tradesToday               int
	maxTradesPerTickerPerDay  int
}

// NewTickerWorker создаёт изолированный воркер для тикера.
func NewTickerWorker(
	ticker string,
	exp config.ResolvedExperiment,
	tickerCount int,
	stepPriceValue float64,
	strategyOpts strategy.Options,
	sessionCfg config.SessionConfig,
	tradingMode, runID, classCode, candleTimeframe string,
	store interfaces.TradeStore,
) (*TickerWorker, error) {
	clock, err := NewSessionClock(sessionCfg.Timezone, sessionCfg.EODCloseTime, sessionCfg.SessionOpenTime, sessionCfg.EntryDelayMinutes)
	if err != nil {
		return nil, err
	}

	if stepPriceValue <= 0 {
		stepPriceValue = 1.0
	}
	if store == nil {
		store = interfaces.NoopTradeStore{}
	}

	deposit := exp.PerTickerDeposit(tickerCount)
	maxLoss := exp.PerTickerMaxDailyLoss(tickerCount)
	label := ticker
	if exp.ID != "" && exp.ID != "default" {
		label = fmt.Sprintf("%s/%s", exp.ID, ticker)
	}

	return &TickerWorker{
		label:            label,
		ticker:           ticker,
		experimentID:     exp.ID,
		stopMode:         strategyOpts.StopMode,
		stepPriceValue:   stepPriceValue,
		tradingMode:      tradingMode,
		runID:            runID,
		classCode:        classCode,
		candleTimeframe:  candleTimeframe,
		lookback:         strategyOpts.Lookback,
		riskPerTradePct:  exp.Risk.RiskPerTradePercent,
		depositPerTicker: deposit,
		strategy:         strategy.NewMomentumBreakout(strategyOpts),
		riskMgr:                  risk.NewRiskManager(deposit, maxLoss, exp.Risk.RiskPerTradePercent, stepPriceValue),
		session:                  clock,
		store:                    store,
		maxTradesPerTickerPerDay: exp.Strategy.MaxTradesPerTickerPerDay,
		candleChan:       make(chan models.Candle, 64),
		tickChan:         make(chan models.Tick, 256),
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
			w.checkDailyReset()
			w.checkSLTP(ctx, executor, tick.Price)
			w.checkEOD(ctx, executor, tick.Price)

		case candle, ok := <-w.candleChan:
			if !ok {
				logx.WorkerLifecycle(w.label, "канал свечей закрыт")
				return
			}
			w.lastPrice = candle.Close
			w.checkDailyReset()
			w.checkEOD(ctx, executor, candle.Close)
			w.processCandle(ctx, executor, candle)

		case <-eodTicker.C:
			w.checkDailyReset()
			if w.lastPrice > 0 {
				w.checkEOD(ctx, executor, w.lastPrice)
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

	if !w.session.EntriesAllowed(time.Now()) {
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

	quantity := w.riskMgr.CalculatePositionSize(signal.Price, signal.StopLoss)
	if quantity <= 0 {
		logx.SignalRejected(w.label, signal.Direction, "нулевой объём позиции")
		return
	}

	signal.Quantity = quantity
	signal.OrderType = models.OrderTypeLimit

	if err := executor.ExecuteOrder(ctx, *signal); err != nil {
		logx.Error("[%s] ошибка исполнения ордера: %v", w.label, err)
		return
	}

	logx.TradeOpen(w.label, signal.Direction, signal.Quantity, signal.Price, signal.StopLoss, signal.TakeProfit)
	w.tradesToday++

	w.position = &openPosition{
		direction:         signal.Direction,
		quantity:          signal.Quantity,
		entryPrice:        signal.Price,
		initialStopLoss:   signal.StopLoss,
		initialTakeProfit: signal.TakeProfit,
		stopLoss:          signal.StopLoss,
		takeProfit:        signal.TakeProfit,
		rDistance:         math.Abs(signal.Price - signal.StopLoss),
		trailStage:        0,
		mfePrice:          signal.Price,
		maePrice:          signal.Price,
		breakoutUpper:     signal.BreakoutUpper,
		breakoutLower:     signal.BreakoutLower,
		openedAt:          candle.Timestamp,
	}
}

func (w *TickerWorker) checkSLTP(ctx context.Context, executor interfaces.OrderExecutor, price float64) {
	if w.position == nil {
		return
	}

	updateMFE(w.position, price)
	updateMAE(w.position, price)
	w.updateTrailingStop(price)

	pos := w.position
	switch pos.direction {
	case "BUY":
		if price <= pos.stopLoss {
			w.closePosition(ctx, executor, price, models.CloseReasonStopLoss)
		} else if price >= pos.takeProfit {
			w.closePosition(ctx, executor, price, models.CloseReasonTakeProfit)
		}
	case "SELL":
		if price >= pos.stopLoss {
			w.closePosition(ctx, executor, price, models.CloseReasonStopLoss)
		} else if price <= pos.takeProfit {
			w.closePosition(ctx, executor, price, models.CloseReasonTakeProfit)
		}
	}
}

func (w *TickerWorker) updateTrailingStop(price float64) {
	pos := w.position
	if pos == nil || pos.rDistance <= 0 {
		return
	}

	prevStage := pos.trailStage
	applyTrailingStop(pos, price, w.stepPriceValue)
	if pos.trailStage > prevStage {
		switch pos.trailStage {
		case 1:
			logx.Trailing(w.label, 1, pos.stopLoss)
		case 2:
			logx.Trailing(w.label, 2, pos.stopLoss)
		}
	}
}

func updateMFE(pos *openPosition, price float64) {
	if pos == nil {
		return
	}
	switch pos.direction {
	case "BUY":
		if price > pos.mfePrice {
			pos.mfePrice = price
		}
	case "SELL":
		if price < pos.mfePrice {
			pos.mfePrice = price
		}
	}
}

func updateMAE(pos *openPosition, price float64) {
	if pos == nil {
		return
	}
	switch pos.direction {
	case "BUY":
		if price < pos.maePrice {
			pos.maePrice = price
		}
	case "SELL":
		if price > pos.maePrice {
			pos.maePrice = price
		}
	}
}

func calcMFEinR(pos *openPosition) float64 {
	if pos == nil || pos.rDistance <= 0 {
		return 0
	}
	switch pos.direction {
	case "BUY":
		return (pos.mfePrice - pos.entryPrice) / pos.rDistance
	case "SELL":
		return (pos.entryPrice - pos.mfePrice) / pos.rDistance
	default:
		return 0
	}
}

func calcMAEinR(pos *openPosition) float64 {
	if pos == nil || pos.rDistance <= 0 {
		return 0
	}
	switch pos.direction {
	case "BUY":
		return (pos.entryPrice - pos.maePrice) / pos.rDistance
	case "SELL":
		return (pos.maePrice - pos.entryPrice) / pos.rDistance
	default:
		return 0
	}
}

// applyTrailingStop подтягивает SL при +1R и +2R; после +2R — непрерывный трейлинг SL = MFE − 1R.
func applyTrailingStop(pos *openPosition, price, stepPriceValue float64) {
	if pos == nil || pos.rDistance <= 0 || stepPriceValue <= 0 {
		return
	}

	breakevenOffset := virtualCommissionPerLot / stepPriceValue

	switch pos.direction {
	case "BUY":
		if pos.trailStage < 1 && price >= pos.entryPrice+pos.rDistance {
			newSL := pos.entryPrice + breakevenOffset
			if newSL > pos.stopLoss {
				pos.stopLoss = newSL
			}
			pos.trailStage = 1
		}
		if pos.trailStage < 2 && price >= pos.entryPrice+2*pos.rDistance {
			newSL := pos.entryPrice + pos.rDistance
			if newSL > pos.stopLoss {
				pos.stopLoss = newSL
			}
			pos.trailStage = 2
		}
	case "SELL":
		if pos.trailStage < 1 && price <= pos.entryPrice-pos.rDistance {
			newSL := pos.entryPrice - breakevenOffset
			if newSL < pos.stopLoss {
				pos.stopLoss = newSL
			}
			pos.trailStage = 1
		}
		if pos.trailStage < 2 && price <= pos.entryPrice-2*pos.rDistance {
			newSL := pos.entryPrice - pos.rDistance
			if newSL < pos.stopLoss {
				pos.stopLoss = newSL
			}
			pos.trailStage = 2
		}
	}

	if pos.trailStage >= 2 {
		switch pos.direction {
		case "BUY":
			newSL := pos.mfePrice - pos.rDistance
			if newSL > pos.stopLoss {
				pos.stopLoss = newSL
			}
		case "SELL":
			newSL := pos.mfePrice + pos.rDistance
			if newSL < pos.stopLoss {
				pos.stopLoss = newSL
			}
		}
	}
}

func (w *TickerWorker) checkEOD(ctx context.Context, executor interfaces.OrderExecutor, price float64) {
	now := time.Now()
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

func (w *TickerWorker) checkDailyReset() {
	now := time.Now()
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
	if pos.direction == "SELL" {
		closeDir = "BUY"
	}

	order := models.Order{
		Ticker:      w.ticker,
		Direction:   closeDir,
		Quantity:    pos.quantity,
		Price:       price,
		OrderType:   models.OrderTypeMarket,
		CloseReason: reason,
	}

	if err := executor.ExecuteOrder(ctx, order); err != nil {
		logx.Error("[%s] ошибка закрытия позиции (%s): %v", w.label, reason, err)
		w.position = pos
		return
	}

	pnl := calcPnL(pos, price, w.stepPriceValue)
	if pnl < 0 {
		w.riskMgr.RegisterLoss(-pnl)
	} else if pnl > 0 {
		w.riskMgr.RegisterProfit(pnl)
	}

	closedAt := time.Now()
	riskAmount := pos.rDistance * float64(pos.quantity) * w.stepPriceValue
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
		Direction:         pos.direction,
		Quantity:          pos.quantity,
		EntryPrice:        pos.entryPrice,
		ExitPrice:         price,
		InitialStopLoss:   pos.initialStopLoss,
		InitialTakeProfit: pos.initialTakeProfit,
		FinalStopLoss:     pos.stopLoss,
		RDistance:         pos.rDistance,
		GrossPnL:          pnl,
		PnLR:              pnlR,
		MFEinR:            calcMFEinR(pos),
		MAEinR:            calcMAEinR(pos),
		BreakoutUpper:     pos.breakoutUpper,
		BreakoutLower:     pos.breakoutLower,
		CloseReason:       reason,
		TrailStage:        pos.trailStage,
		IsWinner:          pnl > 0,
		OpenedAt:          pos.openedAt,
		ClosedAt:          closedAt,
		HoldSeconds:       int(closedAt.Sub(pos.openedAt).Seconds()),
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

func calcPnL(pos *openPosition, closePrice, stepPriceValue float64) float64 {
	qty := float64(pos.quantity)
	switch pos.direction {
	case "BUY":
		return (closePrice - pos.entryPrice) * qty * stepPriceValue
	case "SELL":
		return (pos.entryPrice - closePrice) * qty * stepPriceValue
	default:
		return 0
	}
}
