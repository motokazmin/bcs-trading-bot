package app

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"bcs-trading-bot/internal/config"
	"bcs-trading-bot/internal/engine/broker"
	"bcs-trading-bot/internal/engine/contract"
	"bcs-trading-bot/internal/engine/execution"
	"bcs-trading-bot/internal/engine/risk"
	"bcs-trading-bot/internal/engine/storage/sqlite"
	"bcs-trading-bot/internal/logx"
)

// Dependencies — технические сущности каркаса, общие для всех стратегий:
// исполнитель ордеров, хранилище сделок, портфельный риск-контроллер.
type Dependencies struct {
	Executor  contract.OrderExecutor
	Store     contract.TradeStore
	Reader    contract.TradeReader // nil, если storage выключен
	Portfolio *risk.GlobalRiskController
	RunID     string

	closers []func()
}

// Close освобождает ресурсы (БД). Вызывать через defer в main.
func (d *Dependencies) Close() {
	for i := len(d.closers) - 1; i >= 0; i-- {
		d.closers[i]()
	}
}

// BuildDependencies поднимает хранилище, портфельный риск и исполнителя
// ордеров под режим торговли из конфига. При ошибке — logx.Fatal.
func BuildDependencies(ctx context.Context, opts Options, cfg *config.Config, client *broker.BCSClient) *Dependencies {
	d := &Dependencies{
		RunID: fmt.Sprintf("%s-%s", filepath.Base(opts.ConfigPath), time.Now().Format("20060102-150405")),
	}

	d.Store = contract.NoopTradeStore{}
	if cfg.StorageEnabled() {
		store, err := sqlite.Open(cfg.Storage.Path)
		if err != nil {
			logx.Fatalf("ошибка открытия БД сделок: %v", err)
		}
		d.Store = store
		d.Reader = store
		d.closers = append(d.closers, func() {
			if err := store.Close(); err != nil {
				logx.Warn("ошибка закрытия БД: %v", err)
			}
		})
		logx.Info("Хранилище сделок: %s", cfg.Storage.Path)
	}

	d.Portfolio = buildPortfolioRisk(cfg)
	d.Executor = buildExecutor(ctx, cfg, client)
	return d
}

func buildPortfolioRisk(cfg *config.Config) *risk.GlobalRiskController {
	accountRisk := cfg.AccountRisk()
	maxParallel := accountRisk.MaxParallelTrades
	if maxParallel <= 0 {
		maxParallel = 5
	}
	riskPct := accountRisk.RiskPerTradePercent
	if riskPct <= 0 {
		riskPct = 0.5
	}
	pct := accountRisk.MaxDailyLossPercent
	if pct <= 0 {
		pct = 2.0
	}
	globalRisk := risk.NewGlobalRiskController(accountRisk.Deposit, pct, riskPct, maxParallel)
	logx.Info(
		"Единый счёт: депозит %.0f | CB %.1f%% | open_risk_budget=%.0f ₽ (%.1f%%×%d слотов) | one-position-per-ticker",
		accountRisk.Deposit,
		pct,
		globalRisk.MaxOpenRiskBudgetLimit(),
		riskPct,
		maxParallel,
	)
	return globalRisk
}

func buildExecutor(ctx context.Context, cfg *config.Config, client *broker.BCSClient) contract.OrderExecutor {
	switch cfg.TradingMode {
	case config.TradingModeVirtual:
		balance := cfg.AccountBalance()
		logx.Mode(true, fmt.Sprintf("баланс %.0f руб.", balance))
		return execution.NewVirtualExecutor(balance)

	case config.TradingModeReal:
		if len(cfg.ResolvedExperiments()) != 1 {
			logx.Fatal("real mode: ожидается ровно один эксперимент")
		}
		client.SetWriteMode()
		if err := client.Connect(ctx); err != nil {
			logx.Fatalf("авторизация (write) провалена: %v", err)
		}
		logx.Mode(false, "BCSClient (trade-api-write)")
		if balance, err := client.GetBalance(ctx); err != nil {
			logx.Warn("не удалось получить баланс: %v", err)
		} else {
			logx.Info("Баланс счёта: %.2f руб.", balance)
		}
		return client

	default:
		logx.Fatalf("неизвестный trading_mode: %q", cfg.TradingMode)
		return nil // недостижимо: logx.Fatalf вызывает os.Exit
	}
}
