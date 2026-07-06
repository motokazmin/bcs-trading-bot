package config_test

import (
	"testing"

	"bcs-trading-bot/internal/config"
)

func TestLoadExperimentsAll(t *testing.T) {
	cfg, err := config.Load("../../configs/experiments-all.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.TradingMode != config.TradingModeVirtual {
		t.Fatalf("trading_mode: got %q", cfg.TradingMode)
	}
	exps := cfg.ResolvedExperiments()
	if len(exps) != 6 {
		t.Fatalf("experiments: got %d, want 6", len(exps))
	}
	if len(cfg.AllTickerSymbols()) != 9 {
		t.Fatalf("all tickers: got %d, want 9", len(cfg.AllTickerSymbols()))
	}
	if len(cfg.TickersForExperiment(exps[0])) != 3 {
		t.Fatalf("atr-2-lean tickers: got %d, want 3", len(cfg.TickersForExperiment(exps[0])))
	}
	if cfg.SessionForExperiment(exps[0]).EntryDelayMinutes != 0 {
		t.Fatalf("atr-2-lean delay: got %d, want 0", cfg.SessionForExperiment(exps[0]).EntryDelayMinutes)
	}
	if cfg.SessionForExperiment(exps[2]).EntryDelayMinutes != 30 {
		t.Fatalf("atr-2-delayed delay: got %d, want 30", cfg.SessionForExperiment(exps[2]).EntryDelayMinutes)
	}
	if len(cfg.TickersForExperiment(exps[2])) != 9 {
		t.Fatalf("atr-2-delayed tickers: got %d, want 9", len(cfg.TickersForExperiment(exps[2])))
	}

	vol := exps[3]
	if vol.ID != "atr-2-lean-vol" || !vol.Strategy.VolumeFilterEnabled() {
		t.Fatalf("atr-2-lean-vol: %+v", vol)
	}
	if vol.Strategy.VolumeMinRatio != 1.5 {
		t.Fatalf("volume_min_ratio: got %f", vol.Strategy.VolumeMinRatio)
	}
	if len(cfg.TickersForExperiment(vol)) != 9 {
		t.Fatalf("atr-2-lean-vol tickers: got %d, want 9", len(cfg.TickersForExperiment(vol)))
	}
	if exps[5].ID != "atr-2-delayed-vol" || cfg.SessionForExperiment(exps[5]).EntryDelayMinutes != 30 {
		t.Fatalf("atr-2-delayed-vol: %+v", exps[5])
	}
}

func TestLoadRealStocks(t *testing.T) {
	cfg, err := config.Load("../../configs/real-stocks.yaml")
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
