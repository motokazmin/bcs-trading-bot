package config_test

import (
	"testing"

	"bcs-trading-bot/internal/config"
)

func TestLoadVirtualSber(t *testing.T) {
	cfg, err := config.Load("../../configs/virtual-sber.yaml")
	if err != nil {
		t.Fatal(err)
	}

	if cfg.TradingMode != config.TradingModeVirtual {
		t.Fatalf("trading_mode: got %q", cfg.TradingMode)
	}
	if len(cfg.Tickers) != 1 || cfg.Tickers[0].Symbol != "SBER" {
		t.Fatalf("tickers: %v", cfg.Tickers)
	}
	if cfg.Tickers[0].StepPriceValue != 1.0 {
		t.Fatalf("step_price_value: got %f, want 1.0", cfg.Tickers[0].StepPriceValue)
	}
	if cfg.ClassCode != "TQBR" {
		t.Fatalf("class_code: %q", cfg.ClassCode)
	}
	if cfg.PerTickerDeposit() != cfg.Risk.Deposit {
		t.Fatalf("per-ticker deposit: %f", cfg.PerTickerDeposit())
	}
}

func TestLoadMultiTickerSplit(t *testing.T) {
	cfg, err := config.Load("../../configs/virtual-multi.yaml")
	if err != nil {
		t.Fatal(err)
	}

	if len(cfg.Tickers) != 10 {
		t.Fatalf("expected 10 tickers, got %d", len(cfg.Tickers))
	}
	if cfg.PerTickerDeposit() != 20_000 {
		t.Fatalf("expected 20000 per ticker, got %f", cfg.PerTickerDeposit())
	}
	if cfg.PerTickerMaxDailyLoss() != 400 {
		t.Fatalf("expected 400 max loss per ticker, got %f", cfg.PerTickerMaxDailyLoss())
	}
}

func TestLoadExperimentsMulti(t *testing.T) {
	cfg, err := config.Load("../../configs/experiments-multi.yaml")
	if err != nil {
		t.Fatal(err)
	}

	if !cfg.HasExperiments() {
		t.Fatal("expected experiments section")
	}
	if len(cfg.ResolvedExperiments()) != 3 {
		t.Fatalf("experiments: got %d, want 3", len(cfg.ResolvedExperiments()))
	}
	if len(cfg.Tickers) != 10 {
		t.Fatalf("tickers: got %d, want 10", len(cfg.Tickers))
	}

	exp := cfg.ResolvedExperiments()[1]
	if exp.ID != "atr-2" {
		t.Fatalf("experiment id: %q", exp.ID)
	}
	if exp.Strategy.StopMode != "atr" {
		t.Fatalf("stop_mode: %q", exp.Strategy.StopMode)
	}
	if exp.Strategy.ATRMultiplier != 2.0 {
		t.Fatalf("atr_multiplier: %f", exp.Strategy.ATRMultiplier)
	}
	if exp.PerTickerDeposit(10) != 20_000 {
		t.Fatalf("per-ticker deposit: %f", exp.PerTickerDeposit(10))
	}
}

func TestLoadFuturesStepPriceValue(t *testing.T) {
	cfg, err := config.Load("../../configs/virtual-futures.yaml")
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
