package config_test

import (
	"testing"

	"bcs-trading-bot/internal/config"
)

func TestLoadORFadeChampion(t *testing.T) {
	cfg, err := config.Load("../../configs/champions/or-fade-wave3-afks.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Strategy.Type != "opening_range_fade" {
		t.Fatalf("type: got %q", cfg.Strategy.Type)
	}
	if cfg.Strategy.Int("fade_window_minutes") != 52 {
		t.Fatalf("fade_window_minutes: got %d, want 52", cfg.Strategy.Int("fade_window_minutes"))
	}
	if cfg.Strategy.Int("fade_trade_end_minutes") != 106 {
		t.Fatalf("fade_trade_end_minutes: got %d, want 106", cfg.Strategy.Int("fade_trade_end_minutes"))
	}
	rir := cfg.Strategy.BoolPtr("require_inside_range")
	if rir == nil || *rir {
		t.Fatal("require_inside_range: want false")
	}
	s, err := cfg.Strategy.BuildStrategy(cfg.Session)
	if err != nil {
		t.Fatal(err)
	}
	if s.ID() != "opening_range_fade" {
		t.Fatalf("strategy id: got %q", s.ID())
	}
}

func TestLoadPortfolioPaper(t *testing.T) {
	cfg, err := config.Load("../../configs/runs/portfolio-paper.yaml")
	if err != nil {
		t.Fatal(err)
	}
	exps := cfg.ResolvedExperiments()
	if len(exps) != 5 {
		t.Fatalf("experiments: got %d, want 5", len(exps))
	}
	if cfg.AccountRisk().Deposit != 200_000 {
		t.Fatalf("account deposit: got %.0f, want 200000", cfg.AccountRisk().Deposit)
	}
}

func TestLoadRealStocks(t *testing.T) {
	cfg, err := config.Load("../../configs/runs/real-stocks.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.TradingMode != config.TradingModeReal {
		t.Fatalf("trading_mode: got %q", cfg.TradingMode)
	}
	if len(cfg.Tickers) != 1 || cfg.Tickers[0].Symbol != "SBER" {
		t.Fatalf("tickers: %v", cfg.Tickers)
	}
}

func TestLoadFuturesStepPriceValue(t *testing.T) {
	cfg, err := config.Load("../../configs/runs/virtual-futures.yaml")
	if err != nil {
		t.Fatal(err)
	}

	if len(cfg.Tickers) != 2 {
		t.Fatalf("expected 2 tickers, got %d", len(cfg.Tickers))
	}
	if cfg.Tickers[0].Symbol != "SRH6" || cfg.Tickers[0].StepPriceValue != 1.2 {
		t.Fatalf("SRH6: got %+v", cfg.Tickers[0])
	}
	if cfg.Tickers[1].Symbol != "GAZR" || cfg.Tickers[1].StepPriceValue != 1.0 {
		t.Fatalf("GAZR: got %+v", cfg.Tickers[1])
	}
	if cfg.CommissionPerLot() != 5.0 {
		t.Fatalf("commission_per_lot: got %v, want 5.0", cfg.CommissionPerLot())
	}
}

func TestConfigCommissionDefaultByClassCode(t *testing.T) {
	const yamlData = `
trading_mode: virtual
tickers: [SBER]
class_code: TQBR
risk:
  deposit: 100000
  max_daily_loss_percent: 2
`
	cfg, err := config.LoadFromBytes([]byte(yamlData))
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.CommissionPerLot(); got != 0 {
		t.Fatalf("TQBR default flat commission: got %v, want 0 (rate model)", got)
	}
	if !cfg.CostsConfig().UsesRate("TQBR") {
		t.Fatal("expected default rate commission model for TQBR")
	}
}

func TestTickerConfigUnmarshalObject(t *testing.T) {
	const yamlData = `
trading_mode: virtual
tickers:
  - ticker: SRH6
    step_price_value: 2.5
risk:
  deposit: 100000
  max_daily_loss_percent: 2
`

	cfg, err := config.LoadFromBytes([]byte(yamlData))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Tickers[0].Symbol != "SRH6" {
		t.Fatalf("symbol: %q", cfg.Tickers[0].Symbol)
	}
	if cfg.Tickers[0].StepPriceValue != 2.5 {
		t.Fatalf("step_price_value: %f", cfg.Tickers[0].StepPriceValue)
	}
}
