package risk_test

import (
	"testing"

	"bcs-trading-bot/internal/risk"
)

func TestResetDailyUnblocks(t *testing.T) {
	rm := risk.NewRiskManager(100_000, 2_000, 0.5, 1.0)
	rm.RegisterLoss(2_500)

	if err := rm.CheckCircuitBreaker(); err == nil {
		t.Fatal("expected circuit breaker to be active")
	}

	rm.ResetDaily()

	if err := rm.CheckCircuitBreaker(); err != nil {
		t.Fatalf("expected trading to be allowed after reset: %v", err)
	}
}

func TestRegisterProfitDoesNotUnblock(t *testing.T) {
	rm := risk.NewRiskManager(100_000, 2_000, 0.5, 1.0)
	rm.RegisterLoss(2_500)

	if err := rm.CheckCircuitBreaker(); err == nil {
		t.Fatal("expected circuit breaker to be active after loss")
	}

	rm.RegisterProfit(3_000)

	if err := rm.CheckCircuitBreaker(); err == nil {
		t.Fatal("expected circuit breaker to remain active after profit")
	}
}

func TestCalculatePositionSizeWithStepPrice(t *testing.T) {
	rm := risk.NewRiskManager(100_000, 2_000, 0.5, 1.2)

	// risk = 0.5% of 100_000 = 500₽; price risk = 1 pt; step = 1.2 → 500/(1*1.2) = 416
	got := rm.CalculatePositionSize(100, 99)
	if got != 416 {
		t.Fatalf("position size: got %d, want 416", got)
	}
}

func TestCalculatePositionSizeStocksDefaultStep(t *testing.T) {
	rm := risk.NewRiskManager(100_000, 2_000, 0.5, 1.0)

	// risk = 500₽; price risk = 1₽ → 500 lots
	got := rm.CalculatePositionSize(100, 99)
	if got != 500 {
		t.Fatalf("position size: got %d, want 500", got)
	}
}

func TestCapQuantityByCash(t *testing.T) {
	if got := risk.CapQuantityByCash(500, 100, 20_000); got != 200 {
		t.Fatalf("cap: got %d, want 200", got)
	}
	if got := risk.CapQuantityByCash(100, 100, 50_000); got != 100 {
		t.Fatalf("no-op: got %d, want 100", got)
	}
	if got := risk.CapQuantityByCash(100, 1658, 1000); got != 0 {
		t.Fatalf("too little cash: got %d, want 0", got)
	}
	if got := risk.CapQuantityByCash(50, 100, 0); got != 0 {
		t.Fatalf("zero cash: got %d, want 0", got)
	}
}
