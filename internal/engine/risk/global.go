package risk

import (
	"errors"
	"sync"
)

// ErrCircuitBreakerTriggered — дневной лимит убытков (2%) исчерпан, новые сделки запрещены.
var ErrCircuitBreakerTriggered = errors.New("circuit breaker triggered: daily loss limit reached")

// ErrMaxRiskBudgetExceeded — превышен лимит суммарного открытого риска по портфелю.
var ErrMaxRiskBudgetExceeded = errors.New("max open risk budget exceeded")

// ErrMaxParallelTrades — устаревшее имя ErrMaxRiskBudgetExceeded (обратная совместимость).
var ErrMaxParallelTrades = ErrMaxRiskBudgetExceeded

// ErrTickerBusy — по тикеру уже есть открытая позиция (одна позиция на тикер).
var ErrTickerBusy = errors.New("ticker busy: position already open")

// OpenPosition — открытая позиция для учёта в глобальном риск-контроллере.
type OpenPosition struct {
	Ticker string
	Risk   float64 // максимальный риск в рублях при срабатывании SL
}

// GlobalRiskController — портфельный контроллер риска (circuit breaker + лимит открытого риска).
type GlobalRiskController struct {
	mu                sync.RWMutex
	deposit           float64
	maxDailyLoss      float64
	maxOpenRiskBudget float64 // суммарный риск открытых позиций в рублях (SL-notional)
	realizedPnL       float64
	openPositions     map[string]float64 // ticker -> risk amount
	blocked           bool
	tradingDate       string
}

// MaxOpenRiskBudget вычисляет лимит открытого риска из депозита, % на сделку и числа слотов.
// maxParallelTrades в конфиге задаёт не count-проверку, а размер бюджета: N × risk%.
func MaxOpenRiskBudget(deposit, riskPerTradePercent float64, maxParallelTrades int) float64 {
	if maxParallelTrades <= 0 {
		maxParallelTrades = 2
	}
	if riskPerTradePercent <= 0 {
		riskPerTradePercent = 0.5
	}
	if deposit <= 0 {
		return 0
	}
	perTrade := deposit * riskPerTradePercent / 100
	return perTrade * float64(maxParallelTrades)
}

// NewGlobalRiskController создаёт глобальный риск-контроллер.
func NewGlobalRiskController(deposit, maxDailyLossPercent, riskPerTradePercent float64, maxParallelTrades int) *GlobalRiskController {
	if maxDailyLossPercent <= 0 {
		maxDailyLossPercent = 2.0
	}
	return &GlobalRiskController{
		deposit:           deposit,
		maxDailyLoss:      deposit * maxDailyLossPercent / 100,
		maxOpenRiskBudget: MaxOpenRiskBudget(deposit, riskPerTradePercent, maxParallelTrades),
		openPositions:     make(map[string]float64),
	}
}

// PreTradeCheck проверяет circuit breaker перед открытием новой позиции.
// Учитывает реализованный PnL, риск открытых позиций и риск новой сделки.
func (g *GlobalRiskController) PreTradeCheck(newTradeRisk float64) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.blocked {
		return ErrCircuitBreakerTriggered
	}

	totalOpenRisk := g.sumOpenRiskLocked()
	exposure := g.realizedPnL - totalOpenRisk - newTradeRisk
	if exposure <= -g.maxDailyLoss {
		g.blocked = true
		return ErrCircuitBreakerTriggered
	}
	return nil
}

// CanOpenPosition проверяет, что новая сделка с риском newTradeRisk укладывается в бюджет.
func (g *GlobalRiskController) CanOpenPosition(newTradeRisk float64) error {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if g.sumOpenRiskLocked()+newTradeRisk > g.maxOpenRiskBudget {
		return ErrMaxRiskBudgetExceeded
	}
	return nil
}

// CanOpenTicker проверяет, что по тикеру ещё нет открытой позиции.
func (g *GlobalRiskController) CanOpenTicker(ticker string) error {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if _, ok := g.openPositions[ticker]; ok {
		return ErrTickerBusy
	}
	return nil
}

// TryOpen атомарно проверяет CB + risk budget + ticker busy и резервирует риск.
// При ошибке исполнения ордера вызывающий обязан ReleaseOpen.
func (g *GlobalRiskController) TryOpen(ticker string, newTradeRisk float64) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.blocked {
		return ErrCircuitBreakerTriggered
	}
	if g.sumOpenRiskLocked()+newTradeRisk > g.maxOpenRiskBudget {
		return ErrMaxRiskBudgetExceeded
	}
	if _, ok := g.openPositions[ticker]; ok {
		return ErrTickerBusy
	}

	totalOpenRisk := g.sumOpenRiskLocked()
	exposure := g.realizedPnL - totalOpenRisk - newTradeRisk
	if exposure <= -g.maxDailyLoss {
		g.blocked = true
		return ErrCircuitBreakerTriggered
	}

	g.openPositions[ticker] = newTradeRisk
	return nil
}

// RegisterOpen регистрирует открытие позиции.
// Предпочтительно TryOpen (атомарный check+register); RegisterOpen оставлен для тестов/бэктеста.
func (g *GlobalRiskController) RegisterOpen(ticker string, riskAmount float64) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.openPositions[ticker] = riskAmount
}

// AdjustOpenRisk обновляет зарезервированный риск по уже открытой позиции —
// нужен для частичной фиксации прибыли (partial exits, см. Фазу 3/4, ADR
// 0001): как только объём позиции уменьшается, риск по остатку падает, и
// newRiskAmount < текущего сразу освобождает бюджет для новой сделки, не
// дожидаясь полного закрытия через RegisterClose. Вызов для
// незарегистрированного тикера — no-op, не паника (это ошибка вызывающего
// кода, но не тот случай, где стоит валить торговый цикл).
func (g *GlobalRiskController) AdjustOpenRisk(ticker string, newRiskAmount float64) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if _, ok := g.openPositions[ticker]; !ok {
		return
	}
	g.openPositions[ticker] = newRiskAmount
}

// ReleaseOpen снимает резерв без учёта PnL (откат после неудачного ExecuteOrder).
func (g *GlobalRiskController) ReleaseOpen(ticker string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.openPositions, ticker)
}

// RegisterClose снимает позицию и учитывает реализованный PnL.
func (g *GlobalRiskController) RegisterClose(ticker string, pnl float64) {
	g.mu.Lock()
	defer g.mu.Unlock()

	delete(g.openPositions, ticker)
	g.realizedPnL += pnl

	totalOpenRisk := g.sumOpenRiskLocked()
	if g.realizedPnL-totalOpenRisk <= -g.maxDailyLoss {
		g.blocked = true
	}
}

// ResetDaily сбрасывает дневные счётчики (вызывать в начале торговой сессии).
func (g *GlobalRiskController) ResetDaily(tradingDate string) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.tradingDate == tradingDate {
		return
	}
	g.tradingDate = tradingDate
	g.realizedPnL = 0
	g.openPositions = make(map[string]float64)
	g.blocked = false
}

// IsBlocked возвращает true, если circuit breaker активен.
func (g *GlobalRiskController) IsBlocked() bool {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.blocked
}

// OpenPositionCount возвращает число открытых позиций.
func (g *GlobalRiskController) OpenPositionCount() int {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return len(g.openPositions)
}

// OpenRiskUsed возвращает суммарный риск открытых позиций в рублях.
func (g *GlobalRiskController) OpenRiskUsed() float64 {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.sumOpenRiskLocked()
}

// MaxOpenRiskBudgetLimit возвращает лимит суммарного открытого риска в рублях.
func (g *GlobalRiskController) MaxOpenRiskBudgetLimit() float64 {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.maxOpenRiskBudget
}

func (g *GlobalRiskController) sumOpenRiskLocked() float64 {
	var total float64
	for _, r := range g.openPositions {
		total += r
	}
	return total
}
