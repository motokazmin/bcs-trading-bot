package risk

import (
	"errors"
	"math"
	"sync"
)

type RiskManager struct {
	mu              sync.RWMutex
	maxDailyLoss    float64
	currentLoss     float64
	isBlocked       bool
	accountDeposit  float64
	riskPerTradePct float64
	stepPriceValue  float64
}

func NewRiskManager(deposit, maxLoss, riskPerTradePct, stepPriceValue float64) *RiskManager {
	if riskPerTradePct <= 0 {
		riskPerTradePct = 0.5
	}
	if stepPriceValue <= 0 {
		stepPriceValue = 1.0
	}
	return &RiskManager{
		accountDeposit:  deposit,
		maxDailyLoss:    maxLoss,
		riskPerTradePct: riskPerTradePct,
		stepPriceValue:  stepPriceValue,
	}
}

func (rm *RiskManager) CheckCircuitBreaker() error {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	if rm.isBlocked || rm.currentLoss >= rm.maxDailyLoss {
		return errors.New("risk manager: дневной лимит убытков превышен, торговля заблокирована")
	}
	return nil
}

func (rm *RiskManager) CalculatePositionSize(entryPrice, stopLossPrice float64) int {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	riskAmount := rm.accountDeposit * rm.riskPerTradePct / 100
	priceRisk := math.Abs(entryPrice - stopLossPrice)

	if priceRisk == 0 {
		return 0
	}

	riskPerLot := priceRisk * rm.stepPriceValue
	return int(riskAmount / riskPerLot)
}

// CapQuantityByCash ограничивает объём доступным кэшем (BUY notional = price × qty).
// Если price <= 0 или cash <= 0 — возвращает 0.
func CapQuantityByCash(qty int, price, cash float64) int {
	if qty <= 0 || price <= 0 || cash <= 0 {
		return 0
	}
	maxQty := int(math.Floor(cash / price))
	if maxQty < qty {
		return maxQty
	}
	return qty
}

func (rm *RiskManager) RegisterLoss(amount float64) {
	if amount <= 0 {
		return
	}

	rm.mu.Lock()
	defer rm.mu.Unlock()

	rm.currentLoss += amount
	if rm.currentLoss >= rm.maxDailyLoss {
		rm.isBlocked = true
	}
}

// RegisterProfit не влияет на Circuit Breaker: блокировка держится до ResetDaily.
func (rm *RiskManager) RegisterProfit(amount float64) {
	_ = amount
}

// ResetDaily сбрасывает накопленный дневной убыток и снимает блокировку Circuit Breaker.
func (rm *RiskManager) ResetDaily() {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	rm.currentLoss = 0
	rm.isBlocked = false
}
