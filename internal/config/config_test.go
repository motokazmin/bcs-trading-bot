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
	if len(cfg.Tickers) != 1 || cfg.Tickers[0] != "SBER" {
		t.Fatalf("tickers: %v", cfg.Tickers)
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

	if cfg.PerTickerDeposit() != 100_000 {
		t.Fatalf("expected 100000 per ticker, got %f", cfg.PerTickerDeposit())
	}
	if cfg.PerTickerMaxDailyLoss() != 2000 {
		t.Fatalf("expected 2000 max loss per ticker, got %f", cfg.PerTickerMaxDailyLoss())
	}
}
