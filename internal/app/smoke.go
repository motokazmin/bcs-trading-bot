package app

import (
	"context"

	"bcs-trading-bot/internal/config"
	"bcs-trading-bot/internal/engine"
	"bcs-trading-bot/internal/engine/broker"
	"bcs-trading-bot/internal/engine/execution"
	"bcs-trading-bot/internal/logx"
)

// RunSmoke — быстрая проверка OAuth + WebSocket + виртуальная сделка без
// записи в БД. Только trading_mode: virtual.
func RunSmoke(ctx context.Context, cfg *config.Config, client *broker.BCSClient) {
	if cfg.TradingMode != config.TradingModeVirtual {
		logx.Fatal("smoke-test: только trading_mode: virtual")
	}

	tickers := cfg.AllTickerSymbols()
	if len(tickers) == 0 {
		logx.Fatal("smoke-test: нет тикеров в конфиге")
	}

	executor := execution.NewVirtualExecutor(cfg.AccountBalance())
	if err := engine.RunSmokeTest(ctx, client, tickers[0], executor); err != nil {
		logx.Fatalf("smoke-test провален: %v", err)
	}
}
