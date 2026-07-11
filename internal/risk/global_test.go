package risk

import (
	"sync"
	"testing"
)

func TestGlobalRiskController_CircuitBreaker(t *testing.T) {
	g := NewGlobalRiskController(200_000, 2.0, 2)

	// Реализованный убыток -3000 + открытый риск 1500 = -4500 > лимита -4000
	g.realizedPnL = -3000
	g.openPositions["SBER"] = 1500

	if err := g.PreTradeCheck(0); err != ErrCircuitBreakerTriggered {
		t.Fatalf("expected circuit breaker, got %v", err)
	}
	if !g.IsBlocked() {
		t.Fatal("expected blocked state")
	}
}

func TestGlobalRiskController_MaxParallelTrades(t *testing.T) {
	g := NewGlobalRiskController(200_000, 2.0, 2)
	g.RegisterOpen("SBER", 1000)
	g.RegisterOpen("MGNT", 1000)

	if err := g.CanOpenPosition(); err != ErrMaxParallelTrades {
		t.Fatalf("expected max parallel error, got %v", err)
	}
}

func TestGlobalRiskController_ThreadSafe(t *testing.T) {
	g := NewGlobalRiskController(200_000, 2.0, 2)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ticker := string(rune('A' + (i % 4)))
			_ = g.PreTradeCheck(500)
			g.RegisterOpen(ticker, 500)
			g.RegisterClose(ticker, -100)
		}(i)
	}
	wg.Wait()
}

func TestGlobalRiskController_ResetDaily(t *testing.T) {
	g := NewGlobalRiskController(200_000, 2.0, 2)
	g.realizedPnL = -5000
	g.blocked = true
	g.RegisterOpen("SBER", 1000)

	g.ResetDaily("2024-06-02")
	if g.IsBlocked() {
		t.Fatal("expected unblock after daily reset")
	}
	if g.OpenPositionCount() != 0 {
		t.Fatalf("expected 0 open positions, got %d", g.OpenPositionCount())
	}
}
