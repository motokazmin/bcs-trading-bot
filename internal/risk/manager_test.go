package risk_test

import (
	"testing"

	"bcs-trading-bot/internal/risk"
)

func TestResetDailyUnblocks(t *testing.T) {
	rm := risk.NewRiskManager(100_000, 2_000, 0.5)
	rm.RegisterLoss(2_500)

	if err := rm.CheckCircuitBreaker(); err == nil {
		t.Fatal("expected circuit breaker to be active")
	}

	rm.ResetDaily()

	if err := rm.CheckCircuitBreaker(); err != nil {
		t.Fatalf("expected trading to be allowed after reset: %v", err)
	}
}
