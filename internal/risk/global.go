package risk

import (
	"errors"
	"sync"
)

// ErrCircuitBreakerTriggered — дневной лимит убытков (2%) исчерпан, новые сделки запрещены.
var ErrCircuitBreakerTriggered = errors.New("circuit breaker triggered: daily loss limit reached")

// ErrMaxParallelTrades — превышен лимит одновременных позиций по портфелю.
var ErrMaxParallelTrades = errors.New("max parallel trades limit reached")

// OpenPosition — открытая позиция для учёта в глобальном риск-контроллере.
type OpenPosition struct {
	Ticker string
	Risk   float64 // максимальный риск в рублях при срабатывании SL
}

// GlobalRiskController — портфельный контроллер риска (circuit breaker + лимит позиций).
type GlobalRiskController struct {
	mu                sync.RWMutex
	deposit           float64
	maxDailyLoss      float64
	maxParallelTrades int
	realizedPnL       float64
	openPositions     map[string]float64 // ticker -> risk amount
	blocked           bool
	tradingDate       string
}

// NewGlobalRiskController создаёт глобальный риск-контроллер.
func NewGlobalRiskController(deposit, maxDailyLossPercent float64, maxParallelTrades int) *GlobalRiskController {
	if maxParallelTrades <= 0 {
		maxParallelTrades = 2
	}
	if maxDailyLossPercent <= 0 {
		maxDailyLossPercent = 2.0
	}
	return &GlobalRiskController{
		deposit:           deposit,
		maxDailyLoss:      deposit * maxDailyLossPercent / 100,
		maxParallelTrades: maxParallelTrades,
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

// CanOpenPosition проверяет лимит одновременных позиций.
func (g *GlobalRiskController) CanOpenPosition() error {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if len(g.openPositions) >= g.maxParallelTrades {
		return ErrMaxParallelTrades
	}
	return nil
}

// RegisterOpen регистрирует открытие позиции.
func (g *GlobalRiskController) RegisterOpen(ticker string, riskAmount float64) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.openPositions[ticker] = riskAmount
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

func (g *GlobalRiskController) sumOpenRiskLocked() float64 {
	var total float64
	for _, r := range g.openPositions {
		total += r
	}
	return total
}
