package engine

import (
	"context"
	"log"
	"time"

	"bcs-trading-bot/internal/config"
	"bcs-trading-bot/internal/risk"
	"bcs-trading-bot/internal/strategy"
	"bcs-trading-bot/pkg/interfaces"
	"bcs-trading-bot/pkg/models"
)

type openPosition struct {
	direction  string
	quantity   int
	entryPrice float64
	stopLoss   float64
	takeProfit float64
}

// TickerWorker инкапсулирует торговый цикл для одного тикера.
type TickerWorker struct {
	ticker       string
	strategy     *strategy.MomentumBreakout
	riskMgr      *risk.RiskManager
	session      *SessionClock
	candleChan   chan models.Candle
	tickChan     chan models.Tick
	position       *openPosition
	lastPrice      float64
	eodCloseDate   string
	riskResetDate  string
}

// NewTickerWorker создаёт изолированный воркер для тикера.
func NewTickerWorker(
	ticker string,
	deposit, maxDailyLoss, riskPerTradePct float64,
	lookback int,
	sessionCfg config.SessionConfig,
) (*TickerWorker, error) {
	clock, err := NewSessionClock(sessionCfg.Timezone, sessionCfg.EODCloseTime, sessionCfg.SessionOpenTime)
	if err != nil {
		return nil, err
	}

	return &TickerWorker{
		ticker:     ticker,
		strategy:   strategy.NewMomentumBreakout(lookback),
		riskMgr:    risk.NewRiskManager(deposit, maxDailyLoss, riskPerTradePct),
		session:    clock,
		candleChan: make(chan models.Candle, 64),
		tickChan:   make(chan models.Tick, 256),
	}, nil
}

func (w *TickerWorker) CandleChan() chan<- models.Candle { return w.candleChan }
func (w *TickerWorker) TickChan() chan<- models.Tick     { return w.tickChan }
func (w *TickerWorker) Ticker() string                   { return w.ticker }

// Start запускает цикл обработки тиков, свечей и контроля позиции.
func (w *TickerWorker) Start(ctx context.Context, executor interfaces.OrderExecutor) {
	log.Printf("[%s] воркер запущен", w.ticker)

	eodTicker := time.NewTicker(30 * time.Second)
	defer eodTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Printf("[%s] воркер остановлен", w.ticker)
			return

		case tick, ok := <-w.tickChan:
			if !ok {
				log.Printf("[%s] канал тиков закрыт", w.ticker)
				return
			}
			w.lastPrice = tick.Price
			w.checkDailyReset()
			w.checkSLTP(executor, tick.Price)
			w.checkEOD(executor, tick.Price)

		case candle, ok := <-w.candleChan:
			if !ok {
				log.Printf("[%s] канал свечей закрыт", w.ticker)
				return
			}
			w.lastPrice = candle.Close
			w.checkDailyReset()
			w.checkEOD(executor, candle.Close)
			w.processCandle(ctx, executor, candle)

		case <-eodTicker.C:
			w.checkDailyReset()
			if w.lastPrice > 0 {
				w.checkEOD(executor, w.lastPrice)
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

	signal := w.strategy.OnCandle(candle)
	if signal == nil {
		return
	}

	if err := w.riskMgr.CheckCircuitBreaker(); err != nil {
		log.Printf("[%s] сигнал %s отклонён: %v", w.ticker, signal.Direction, err)
		return
	}

	quantity := w.riskMgr.CalculatePositionSize(signal.Price, signal.StopLoss)
	if quantity <= 0 {
		log.Printf("[%s] сигнал %s отклонён: нулевой объём позиции", w.ticker, signal.Direction)
		return
	}

	signal.Quantity = quantity
	signal.OrderType = models.OrderTypeLimit

	log.Printf(
		"[%s] Сделка валидирована риском, объем %d лотов, отправка | %s %s @ %.2f, SL=%.2f, TP=%.2f",
		w.ticker, signal.Quantity, signal.Direction, signal.Ticker,
		signal.Price, signal.StopLoss, signal.TakeProfit,
	)

	if err := executor.ExecuteOrder(*signal); err != nil {
		log.Printf("[%s] ошибка исполнения ордера: %v", w.ticker, err)
		return
	}

	w.position = &openPosition{
		direction:  signal.Direction,
		quantity:   signal.Quantity,
		entryPrice: signal.Price,
		stopLoss:   signal.StopLoss,
		takeProfit: signal.TakeProfit,
	}
}

func (w *TickerWorker) checkSLTP(executor interfaces.OrderExecutor, price float64) {
	if w.position == nil {
		return
	}

	pos := w.position
	switch pos.direction {
	case "BUY":
		if price <= pos.stopLoss {
			w.closePosition(executor, price, models.CloseReasonStopLoss)
		} else if price >= pos.takeProfit {
			w.closePosition(executor, price, models.CloseReasonTakeProfit)
		}
	case "SELL":
		if price >= pos.stopLoss {
			w.closePosition(executor, price, models.CloseReasonStopLoss)
		} else if price <= pos.takeProfit {
			w.closePosition(executor, price, models.CloseReasonTakeProfit)
		}
	}
}

func (w *TickerWorker) checkEOD(executor interfaces.OrderExecutor, price float64) {
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
		log.Printf("[%s] EOD: принудительное закрытие позиции по %.2f", w.ticker, price)
		w.closePosition(executor, price, models.CloseReasonEOD)
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
	w.riskResetDate = today
	log.Printf("[%s] новый торговый день: дневной счётчик убытков сброшен", w.ticker)
}

func (w *TickerWorker) closePosition(executor interfaces.OrderExecutor, price float64, reason string) {
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

	if err := executor.ExecuteOrder(order); err != nil {
		log.Printf("[%s] ошибка закрытия позиции (%s): %v", w.ticker, reason, err)
		w.position = pos
		return
	}

	pnl := calcPnL(pos, price)
	switch {
	case pnl < 0:
		w.riskMgr.RegisterLoss(-pnl)
	case pnl > 0:
		w.riskMgr.RegisterProfit(pnl)
	}

	log.Printf("[%s] позиция закрыта (%s), PnL=%.2f", w.ticker, reason, pnl)
}

func calcPnL(pos *openPosition, closePrice float64) float64 {
	qty := float64(pos.quantity)
	switch pos.direction {
	case "BUY":
		return (closePrice - pos.entryPrice) * qty
	case "SELL":
		return (pos.entryPrice - closePrice) * qty
	default:
		return 0
	}
}
