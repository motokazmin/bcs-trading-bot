package risk

import (
	"errors"
	"sync"
)

type RiskManager struct {
	mu                 sync.RWMutex
	maxDailyLoss       float64
	currentLoss        float64
	isBlocked          bool
	accountDeposit     float64
	riskPerTradePct    float64
}

func NewRiskManager(deposit, maxLoss, riskPerTradePct float64) *RiskManager {
	if riskPerTradePct <= 0 {
		riskPerTradePct = 0.5
	}
	return &RiskManager{
		accountDeposit:  deposit,
		maxDailyLoss:    maxLoss,
		riskPerTradePct: riskPerTradePct,
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
	priceRisk := entryPrice - stopLossPrice
	if priceRisk < 0 {
		priceRisk = -priceRisk
	}

	if priceRisk == 0 {
		return 0
	}

	return int(riskAmount / priceRisk)
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

func (rm *RiskManager) RegisterProfit(amount float64) {
	if amount <= 0 {
		return
	}

	rm.mu.Lock()
	defer rm.mu.Unlock()

	rm.currentLoss -= amount
	if rm.currentLoss < 0 {
		rm.currentLoss = 0
	}
	if rm.currentLoss < rm.maxDailyLoss {
		rm.isBlocked = false
	}
}

// ResetDaily сбрасывает накопленный дневной убыток и снимает блокировку Circuit Breaker.
func (rm *RiskManager) ResetDaily() {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	rm.currentLoss = 0
	rm.isBlocked = false
}
