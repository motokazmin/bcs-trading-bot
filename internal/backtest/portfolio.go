package backtest

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"bcs-trading-bot/internal/config"
	"bcs-trading-bot/internal/engine"
	"bcs-trading-bot/internal/engine/position"
	"bcs-trading-bot/internal/engine/risk"
	"bcs-trading-bot/internal/strategy"
	"bcs-trading-bot/internal/engine/tradeaudit"
	"bcs-trading-bot/internal/engine/trailing"
	"bcs-trading-bot/internal/engine/contract"
	"bcs-trading-bot/internal/engine/costs"
	"bcs-trading-bot/internal/models"
)

// PortfolioRunnerConfig — параметры портфельной симуляции (несколько слотов, общий риск).
// Ключ Tickers — уникальный slot key (обычно ticker или "experimentID/ticker").
type PortfolioRunnerConfig struct {
	Tickers           map[string]RunnerConfig
	SessionCfg        config.SessionConfig // fallback, если у RunnerConfig.SessionCfg пустая timezone
	GlobalRisk        *risk.GlobalRiskController
	StepPriceByTicker map[string]float64
}

type tickerState struct {
	cfg          RunnerConfig
	strat        strategy.CandleStrategy
	riskMgr      *risk.RiskManager
	session      *engine.SessionClock
	position     *position.State
	tradesToday  int
	lastClose    float64 // close предыдущего бара — цена EOD при дыре в данных
	lastBarEnd   time.Time
	riskResetDate string
}

// PortfolioRunner симулирует несколько слотов с общим глобальным риск-контроллером.
// Несколько стратегий на одном тикере допустимы (разные slot keys); одна позиция на тикер —
// через GlobalRisk.TryOpen.
type PortfolioRunner struct {
	cfg             PortfolioRunnerConfig
	states          map[string]*tickerState
	byTicker        map[string][]*tickerState
	global          *risk.GlobalRiskController
	store           contract.TradeStore
	TickerBusySkips int
}

// NewPortfolioRunner создаёт портфельный симулятор.
func NewPortfolioRunner(cfg PortfolioRunnerConfig, store contract.TradeStore) (*PortfolioRunner, error) {
	if len(cfg.Tickers) == 0 {
		return nil, fmt.Errorf("backtest: portfolio tickers пуст")
	}
	if store == nil {
		store = contract.NoopTradeStore{}
	}

	states := make(map[string]*tickerState, len(cfg.Tickers))
	byTicker := make(map[string][]*tickerState)

	slotKeys := make([]string, 0, len(cfg.Tickers))
	for k := range cfg.Tickers {
		slotKeys = append(slotKeys, k)
	}
	sort.Strings(slotKeys)

	for _, slotKey := range slotKeys {
		rc := cfg.Tickers[slotKey]
		if rc.Ticker == "" {
			rc.Ticker = slotKey
		}
		if rc.StepPriceValue <= 0 {
			rc.StepPriceValue = 1.0
		}
		if rc.Strategy == nil {
			return nil, fmt.Errorf("backtest: strategy не задана для %s", slotKey)
		}
		trailCfg := rc.TrailCfg
		if trailCfg.StepPriceValue <= 0 {
			trailCfg.StepPriceValue = rc.StepPriceValue
		}
		rc.TrailCfg = trailCfg

		sessCfg := rc.SessionCfg
		if sessCfg.Timezone == "" {
			sessCfg = cfg.SessionCfg
		}
		clock, err := engine.NewSessionClockExt(
			sessCfg.Timezone,
			sessCfg.EODCloseTime,
			sessCfg.SessionOpenTime,
			sessCfg.EntryDelayMinutes,
			sessCfg.WeekdaysOnly,
			sessCfg.WeekendOnly,
		)
		if err != nil {
			return nil, fmt.Errorf("backtest: session %s: %w", slotKey, err)
		}

		st := &tickerState{
			cfg:     rc,
			strat:   rc.Strategy,
			riskMgr: risk.NewRiskManager(rc.Deposit, rc.MaxDailyLoss, rc.RiskPerTradePct, rc.StepPriceValue),
			session: clock,
		}
		states[slotKey] = st
		byTicker[rc.Ticker] = append(byTicker[rc.Ticker], st)
	}

	return &PortfolioRunner{
		cfg:        cfg,
		states:     states,
		byTicker:   byTicker,
		global:     cfg.GlobalRisk,
		store:      store,
	}, nil
}

type candleEvent struct {
	ticker string
	candle models.Candle
}

// Run прогоняет свечи всех тикеров в хронологическом порядке.
func (p *PortfolioRunner) Run(ctx context.Context, candlesByTicker map[string][]models.Candle, executor contract.OrderExecutor) error {
	events := p.mergeCandles(candlesByTicker)
	// Бары с одной меткой — в два прохода: сначала выходы по всем тикерам, потом входы.
	// Выход случается внутри бара, вход — на его закрытии; при проходе тикер за тикером
	// вход по A видел ещё открытой позицию по B, закрытую внутри того же бара, и делил
	// с ней лимит параллельных позиций, риск-бюджет и кэш.
	for i := 0; i < len(events); {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		j := i
		for j < len(events) && events[j].candle.Timestamp.Equal(events[i].candle.Timestamp) {
			j++
		}
		for _, ev := range events[i:j] {
			p.processExits(ctx, executor, ev)
		}
		for _, ev := range events[i:j] {
			p.processEntries(ctx, executor, ev)
		}
		i = j
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

func (p *PortfolioRunner) barEnd(ev candleEvent) time.Time {
	slots := p.byTicker[ev.ticker]
	return ev.candle.Timestamp.Add(engine.CandleBarDuration(slots[0].cfg.CandleTimeframe))
}

// processExits — всё, что происходит внутри бара и на его закрытии до входов:
// дневной сброс, пропущенный EOD, SL/TP по пути бара, трейл, EOD на закрытии.
// Решения — на закрытии бара, как в live: метка бара — его начало, время решения —
// конец (docs/analysis/0006, правки 2026-10-03).
func (p *PortfolioRunner) processExits(ctx context.Context, executor contract.OrderExecutor, ev candleEvent) {
	slots := p.byTicker[ev.ticker]
	if len(slots) == 0 {
		return
	}
	candle, barEnd := ev.candle, p.barEnd(ev)
	for _, st := range slots {
		p.checkDailyReset(st, barEnd)
		if st.position != nil {
			p.closeMissedEOD(ctx, executor, st, candle)
		}
		if st.position != nil {
			p.processIntrabar(ctx, executor, st, candle)
		}
		if st.position != nil && st.session.ShouldForceClose(barEnd) {
			p.closePosition(ctx, executor, st, candle.Close, models.CloseReasonEOD, barEnd)
		}
	}
}

// processEntries — входы на закрытии бара (детерминированный порядок слотов).
func (p *PortfolioRunner) processEntries(ctx context.Context, executor contract.OrderExecutor, ev candleEvent) {
	slots := p.byTicker[ev.ticker]
	if len(slots) == 0 {
		return
	}
	candle, barEnd := ev.candle, p.barEnd(ev)
	for _, st := range slots {
		if st.position == nil {
			p.processCandle(ctx, executor, st, candle, barEnd)
		}
		st.lastClose, st.lastBarEnd = candle.Close, barEnd
	}
}

func (p *PortfolioRunner) closeMissedEOD(ctx context.Context, executor contract.OrderExecutor, st *tickerState, candle models.Candle) {
	if st.lastBarEnd.IsZero() {
		return
	}
	newDay := st.session.Today(candle.Timestamp) != st.session.Today(st.position.OpenedAt)
	if newDay || st.session.ShouldForceClose(candle.Timestamp) {
		p.closePosition(ctx, executor, st, st.lastClose, models.CloseReasonEOD, st.lastBarEnd)
	}
}

func (p *PortfolioRunner) processCandle(ctx context.Context, executor contract.OrderExecutor, st *tickerState, candle models.Candle, barEnd time.Time) {
	if !st.session.EntriesAllowed(barEnd) {
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
	qty := st.riskMgr.CalculatePositionSize(signal.Price, signal.StopLoss)
	if qty <= 0 {
		return
	}
	entryAtClose := !signal.IntrabarFill && signal.Price == candle.Close
	// Проскальзывание на входе — см. комментарий в runner.go. Считаем fillPrice
	// ДО капа по кэшу: иначе для BUY (fill дороже сигнала) notional ордера может
	// превысить остаток, закэпленный по старой, более низкой цене (см. живой баг,
	// ADR/фикс от 2026-09-03).
	fillPrice := costs.FillPrice(st.cfg.CostsCfg, signal.Direction, signal.Price)
	// requestedQty/cashAtOpen — см. комментарий в runner.go.
	requestedQty := qty
	cashAtOpen := 0.0
	if bal, err := executor.GetBalance(ctx); err == nil {
		cashAtOpen = bal
		qty = st.cfg.capQuantityByCash(qty, fillPrice, bal)
		if qty <= 0 {
			return
		}
	}
	signal.Quantity = qty
	signal.OrderType = models.OrderTypeLimit
	if signal.Ticker == "" {
		signal.Ticker = st.cfg.Ticker
	}
	signal.Price = fillPrice

	tradeRisk := abs(signal.Price-signal.StopLoss) * float64(qty) * st.cfg.StepPriceValue
	if p.global != nil {
		if err := p.global.TryOpen(st.cfg.Ticker, tradeRisk); err != nil {
			if errors.Is(err, risk.ErrTickerBusy) {
				p.TickerBusySkips++
			}
			return
		}
	}

	if err := executor.ExecuteOrder(ctx, *signal); err != nil {
		if p.global != nil {
			p.global.ReleaseOpen(st.cfg.Ticker)
		}
		return
	}

	st.tradesToday++
	st.position = position.NewFromSignal(*signal, candle.Timestamp)
	st.position.EntryBarTime = candle.Timestamp
	st.position.EntryBarClose = candle.Close
	st.position.EntryAtBarClose = entryAtClose
	st.position.RequestedQuantity = requestedQty
	st.position.CashAtOpen = cashAtOpen

	if reason := position.SameBarExitAfterFill(st.position, candle); reason != "" {
		exitPx := position.ExitFillPrice(st.position, reason, candle.Close)
		st.position.SameBarExit = true
		position.UpdateMAE(st.position, exitPx)
		position.UpdateMFE(st.position, exitPx)
		p.closePosition(ctx, executor, st, exitPx, reason, candle.Timestamp)
	}
}

func (p *PortfolioRunner) processIntrabar(ctx context.Context, executor contract.OrderExecutor, st *tickerState, candle models.Candle) {
	pos := st.position
	for i, price := range position.IntrabarPath(candle, pos.Direction) {
		position.UpdateMFE(pos, price)
		position.UpdateMAE(pos, price)
		if reason := position.CheckExit(pos, price); reason != "" {
			exitPx := position.ExitFillPrice(pos, reason, price)
			if i == 0 { // open — реальная цена: бар мог открыться уже за стопом
				exitPx = position.ExitFillPriceAt(pos, reason, price)
			}
			p.closePosition(ctx, executor, st, exitPx, reason, candle.Timestamp)
			return
		}
	}
	// Трейл — по закрытому бару, как в live (selfmanaged.trailOnBar).
	trailing.Apply(pos, position.TrailPrice(candle, pos.Direction), st.cfg.TrailCfg)
}

// checkDailyReset — дневной сброс слота по ЕГО сессии, как selfmanaged.checkDailyReset
// в live; портфельный контроллер сбрасывается первым открывшимся слотом (в live —
// globalDailyResetLoop каждого раннера, ResetDaily идемпотентен по дате). Раньше всё
// сбрасывалось по часам первого по алфавиту слота (mf-afternoon, 10:00), и дневной
// лимит считался с 10:00, а в live — с 07:00 (утренняя сессия).
func (p *PortfolioRunner) checkDailyReset(st *tickerState, now time.Time) {
	if !st.session.IsSessionOpen(now) {
		return
	}
	today := st.session.Today(now)
	if p.global != nil {
		p.global.ResetDaily(today)
	}
	if st.riskResetDate == today {
		return
	}
	st.riskMgr.ResetDaily()
	st.tradesToday = 0
	st.riskResetDate = today
}

func (p *PortfolioRunner) closePosition(ctx context.Context, executor contract.OrderExecutor, st *tickerState, price float64, reason string, closedAt time.Time) {
	if st.position == nil {
		return
	}

	pos := st.position
	st.position = nil

	// price — уже цена исполнения (ExitFillPrice/ExitFillPriceAt у вызывающего).
	price = costs.FillPrice(st.cfg.CostsCfg, costs.CloseSide(pos.Direction), price)

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
		// Комиссия списывается с кэша исполнителя, как в live (selfmanaged.closePosition).
		// Без неё кэш backtest рос на сумму всех комиссий, кап по кэшу срабатывал иначе, чем
		// в live, и объём расходился у 168 сделок из 1535 (docs/analysis/0010).
		CommissionRub: costs.RoundTrip(st.cfg.CostsCfg, st.cfg.ClassCode, pos.EntryPrice, price, pos.Quantity, st.cfg.StepPriceValue),
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
		TradingMode:        st.cfg.TradingMode,
		RunID:              st.cfg.RunID,
		ExperimentID:       st.cfg.ExperimentID,
		StopMode:           st.cfg.StopMode,
		Ticker:             st.cfg.Ticker,
		ClassCode:          st.cfg.ClassCode,
		StepPriceValue:     st.cfg.StepPriceValue,
		Direction:          pos.Direction,
		Quantity:           pos.Quantity,
		EntryPrice:         pos.EntryPrice,
		ExitPrice:          price,
		InitialStopLoss:    pos.InitialStopLoss,
		InitialTakeProfit:  pos.InitialTakeProfit,
		FinalStopLoss:      pos.StopLoss,
		RDistance:          pos.RDistance,
		GrossPnL:           pnl,
		PnLR:               pnlR,
		MFEinR:             position.CalcMFEinR(pos),
		MAEinR:             position.CalcMAEinR(pos),
		BreakoutUpper:      pos.BreakoutUpper,
		BreakoutLower:      pos.BreakoutLower,
		CloseReason:        reason,
		TrailStage:         pos.TrailStage,
		IsWinner:           pnl > 0,
		OpenedAt:           pos.OpenedAt,
		ClosedAt:           closedAt,
		HoldSeconds:        int(closedAt.Sub(pos.OpenedAt).Seconds()),
		TradingDate:        st.session.Today(closedAt),
		CandleTimeframe:    st.cfg.CandleTimeframe,
		Lookback:           st.cfg.Lookback,
		RiskPerTradePct:    st.cfg.RiskPerTradePct,
		DepositPerTicker:   st.cfg.Deposit,
		StrategyParamsJSON: st.cfg.StrategyParamsJSON,
		EntryBarClose:      pos.EntryBarClose,
		RequestedQuantity:  pos.RequestedQuantity,
		CashAtOpen:         pos.CashAtOpen,
		BarAgeSeconds:      pos.BarAgeSeconds,
	}
	if !pos.EntryBarTime.IsZero() {
		trade.EntryBarTime = pos.EntryBarTime.UTC().Format(time.RFC3339)
	}
	_ = tradeaudit.AnnotateTrade(&trade, tradeaudit.OpenInput{
		Direction:   pos.Direction,
		EntryPrice:  pos.EntryPrice,
		StopLoss:    pos.InitialStopLoss,
		TakeProfit:  pos.InitialTakeProfit,
		RDistance:   pos.RDistance,
		BarClose:    pos.EntryBarClose,
		RewardRatio: st.cfg.RewardRatio,
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
		TrailActivation: st.cfg.TrailCfg.ActivationR,
		HoldSeconds:     trade.HoldSeconds,
		BarDuration:     engine.CandleBarDuration(st.cfg.CandleTimeframe),
		SameBarExit:     pos.SameBarExit,
		EntryBarClose:   pos.EntryBarClose,
	})
	_ = p.store.SaveClosedTrade(ctx, trade)
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}
