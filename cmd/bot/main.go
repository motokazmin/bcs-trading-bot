package main

import (
	"context"
	"os/signal"
	"syscall"

	"bcs-trading-bot/internal/app"
	"bcs-trading-bot/internal/logx"
)

func main() {
	opts := app.ParseFlags()
	defer app.SetupLogging(opts)()

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	cfg := app.MustLoadConfig(opts.ConfigPath)
	client := app.MustConnectBroker(ctx, opts, cfg)

	if opts.SmokeTest {
		app.RunSmoke(ctx, cfg, client)
		return
	}

	deps := app.BuildDependencies(ctx, opts, cfg, client)
	defer deps.Close()

	trader := app.BuildTrader(cfg, client, deps)
	app.StartDashboard(ctx, opts, cfg, trader, deps, client)

	trader.Run(ctx, cancel)

	logx.Info("Завершение работы...")
}
