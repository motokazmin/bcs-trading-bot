package risk

import (
	"sync"
	"testing"
)

func TestMaxOpenRiskBudget(t *testing.T) {
	got := MaxOpenRiskBudget(200_000, 0.5, 5)
	want := 200_000 * 0.005 * 5
	if got != want {
		t.Fatalf("budget: got %.0f, want %.0f", got, want)
	}
}

func TestGlobalRiskController_CircuitBreaker(t *testing.T) {
	g := NewGlobalRiskController(200_000, 2.0, 0.5, 2)

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

func TestGlobalRiskController_MaxOpenRiskBudget(t *testing.T) {
	g := NewGlobalRiskController(200_000, 2.0, 0.5, 2) // budget = 2000
	g.RegisterOpen("SBER", 1000)
	g.RegisterOpen("MGNT", 1000)

	if err := g.CanOpenPosition(1); err != ErrMaxRiskBudgetExceeded {
		t.Fatalf("expected max risk budget error, got %v", err)
	}
}

func TestGlobalRiskController_RiskBudgetAllowsMorePositionsThanSlots(t *testing.T) {
	g := NewGlobalRiskController(200_000, 2.0, 0.5, 2) // budget = 2000, не count=2

	if err := g.TryOpen("SBER", 600); err != nil {
		t.Fatalf("SBER: %v", err)
	}
	if err := g.TryOpen("MGNT", 600); err != nil {
		t.Fatalf("MGNT: %v", err)
	}
	if err := g.TryOpen("TATN", 600); err != nil {
		t.Fatalf("TATN: %v", err)
	}
	if g.OpenPositionCount() != 3 {
		t.Fatalf("open count: got %d, want 3", g.OpenPositionCount())
	}
	if g.OpenRiskUsed() != 1800 {
		t.Fatalf("open risk: got %.0f, want 1800", g.OpenRiskUsed())
	}
	if err := g.TryOpen("CHMF", 300); err != ErrMaxRiskBudgetExceeded {
		t.Fatalf("expected budget exceeded, got %v", err)
	}
}

func TestGlobalRiskController_CanOpenTicker(t *testing.T) {
	g := NewGlobalRiskController(200_000, 2.0, 0.5, 4)
	if err := g.CanOpenTicker("MGNT"); err != nil {
		t.Fatalf("expected free ticker, got %v", err)
	}
	g.RegisterOpen("MGNT", 1000)
	if err := g.CanOpenTicker("MGNT"); err != ErrTickerBusy {
		t.Fatalf("expected ticker busy, got %v", err)
	}
	if err := g.CanOpenTicker("TATN"); err != nil {
		t.Fatalf("expected free TATN, got %v", err)
	}
	g.RegisterClose("MGNT", 100)
	if err := g.CanOpenTicker("MGNT"); err != nil {
		t.Fatalf("expected free after close, got %v", err)
	}
}

func TestGlobalRiskController_TryOpenAtomic(t *testing.T) {
	g := NewGlobalRiskController(200_000, 2.0, 0.5, 4)
	if err := g.TryOpen("CHMF", 1000); err != nil {
		t.Fatalf("first TryOpen: %v", err)
	}
	if err := g.TryOpen("CHMF", 500); err != ErrTickerBusy {
		t.Fatalf("second TryOpen: got %v, want ErrTickerBusy", err)
	}
	g.ReleaseOpen("CHMF")
	if err := g.TryOpen("CHMF", 500); err != nil {
		t.Fatalf("after ReleaseOpen: %v", err)
	}
}

func TestGlobalRiskController_TryOpenRace(t *testing.T) {
	g := NewGlobalRiskController(200_000, 2.0, 0.5, 8)
	var wg sync.WaitGroup
	var okCount int
	var mu sync.Mutex
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := g.TryOpen("CHMF", 100); err == nil {
				mu.Lock()
				okCount++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if okCount != 1 {
		t.Fatalf("concurrent TryOpen wins: got %d, want 1", okCount)
	}
	if g.OpenPositionCount() != 1 {
		t.Fatalf("open count: %d", g.OpenPositionCount())
	}
}

func TestGlobalRiskController_ThreadSafe(t *testing.T) {
	g := NewGlobalRiskController(200_000, 2.0, 0.5, 2)
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
	g := NewGlobalRiskController(200_000, 2.0, 0.5, 2)
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

func TestGlobalRiskController_AdjustOpenRisk(t *testing.T) {
	g := NewGlobalRiskController(200_000, 2.0, 0.5, 2) // budget = 2000
	if err := g.TryOpen("SBER", 1500); err != nil {
		t.Fatalf("TryOpen: %v", err)
	}
	// Частичная фиксация: риск по остатку упал с 1500 до 500.
	g.AdjustOpenRisk("SBER", 500)
	if got := g.OpenRiskUsed(); got != 500 {
		t.Fatalf("open risk after adjust: got %.0f, want 500", got)
	}
	// Освободившийся бюджет должен впустить новую сделку, которую бюджет
	// в 2000 не пропустил бы при старом риске 1500 по SBER.
	if err := g.TryOpen("MGNT", 1400); err != nil {
		t.Fatalf("TryOpen after adjust: %v", err)
	}
	// Несуществующий тикер — no-op, не паника.
	g.AdjustOpenRisk("GHOST", 100)
	if err := g.CanOpenTicker("GHOST"); err != nil {
		t.Fatalf("AdjustOpenRisk must not register unknown ticker: %v", err)
	}
}
