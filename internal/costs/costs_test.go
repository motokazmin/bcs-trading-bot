package costs_test

import (
	"testing"

	"bcs-trading-bot/internal/costs"
)

func TestDefaultPerLotByClass(t *testing.T) {
	if got := costs.DefaultPerLot("TQBR"); got != costs.DefaultPerLotStocks {
		t.Fatalf("TQBR: got %v, want %v", got, costs.DefaultPerLotStocks)
	}
	if got := costs.DefaultPerLot("SPBFUT"); got != costs.DefaultPerLotFutures {
		t.Fatalf("SPBFUT: got %v, want %v", got, costs.DefaultPerLotFutures)
	}
}

func TestConfigPerLotOverride(t *testing.T) {
	cfg := costs.Config{CommissionPerLot: 0.25}
	if got := cfg.PerLot("TQBR"); got != 0.25 {
		t.Fatalf("override: got %v", got)
	}
}

func TestResolveFlag(t *testing.T) {
	cfg := costs.Config{}
	if got := costs.ResolveFlag(0, "TQBR", cfg); got != costs.DefaultPerLotStocks {
		t.Fatalf("auto TQBR: got %v", got)
	}
	if got := costs.ResolveFlag(1.5, "TQBR", cfg); got != 1.5 {
		t.Fatalf("explicit flag: got %v", got)
	}
}

func TestNetPnL(t *testing.T) {
	got := costs.NetPnL(100, 10, 0.10)
	if got != 99 {
		t.Fatalf("net: got %v, want 99", got)
	}
}
