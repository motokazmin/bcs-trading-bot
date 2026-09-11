package costs_test

import (
	"math"
	"testing"

	"bcs-trading-bot/internal/engine/costs"
)

func TestDefaultPerLotByClass(t *testing.T) {
	if got := costs.DefaultPerLot("TQBR"); got != costs.DefaultPerLotStocks {
		t.Fatalf("TQBR: got %v, want %v", got, costs.DefaultPerLotStocks)
	}
	if got := costs.DefaultPerLot("SPBFUT"); got != costs.DefaultPerLotFutures {
		t.Fatalf("SPBFUT: got %v, want %v", got, costs.DefaultPerLotFutures)
	}
}

func TestConfigFlatOverride(t *testing.T) {
	cfg := costs.Config{CommissionPerLot: 0.25}
	if got := cfg.PerLot("TQBR"); got != 0.25 {
		t.Fatalf("flat override: got %v", got)
	}
	if cfg.UsesRate("TQBR") {
		t.Fatal("expected flat model")
	}
}

func TestConfigRateDefault(t *testing.T) {
	cfg := costs.Config{}
	if !cfg.UsesRate("TQBR") {
		t.Fatal("expected rate model for TQBR by default")
	}
	got := costs.RoundTrip(cfg, "TQBR", 7000, 7100, 4, 1)
	want := costs.DefaultCommissionRatePerLeg * (7000 + 7100) * 4
	if math.Abs(got-want) > 1e-9 {
		t.Fatalf("round trip: got %v, want %v", got, want)
	}
}

func TestResolveCostsFlags(t *testing.T) {
	cfg := costs.Config{CommissionRatePerLeg: 0.0001}
	got := costs.ResolveCosts(0.10, -1, "TQBR", cfg)
	if got.CommissionPerLot != 0.10 || got.CommissionRatePerLeg != 0 {
		t.Fatalf("per-lot flag: got %+v", got)
	}
	got = costs.ResolveCosts(-1, 0.00005, "TQBR", costs.Config{})
	if got.CommissionRatePerLeg != 0.00005 || got.CommissionPerLot != 0 {
		t.Fatalf("rate flag: got %+v", got)
	}
}

func TestNetPnLFlat(t *testing.T) {
	cfg := costs.Config{CommissionPerLot: 0.10}
	got := costs.NetPnL(100, cfg, "TQBR", 500, 510, 10, 1)
	if got != 99 {
		t.Fatalf("net flat: got %v, want 99", got)
	}
}

func TestBreakevenOffsetRate(t *testing.T) {
	cfg := costs.Config{CommissionRatePerLeg: 0.00008}
	got := costs.BreakevenOffset(cfg, "TQBR", 7000, 1)
	want := 2 * 0.00008 * 7000
	if got != want {
		t.Fatalf("offset: got %v, want %v", got, want)
	}
}
