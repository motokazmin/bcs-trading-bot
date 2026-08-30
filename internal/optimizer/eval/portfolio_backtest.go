package eval

import (
	"context"
	"fmt"
	"time"

	"bcs-trading-bot/internal/config"
	"bcs-trading-bot/internal/engine/costs"
	"bcs-trading-bot/internal/engine/execution"
	"bcs-trading-bot/internal/engine/risk"
	"bcs-trading-bot/internal/engine/marketdata"
	"bcs-trading-bot/internal/models"
	core "bcs-trading-bot/internal/optimizer/core"
	"bcs-trading-bot/internal/backtest"
	"bcs-trading-bot/internal/engine/storage/memory"
)

// PortfolioBacktestResult — метрики единого счёта по нескольким FROZEN-экспериментам.
type PortfolioBacktestResult struct {
	From            time.Time
	To              time.Time
	Deposit         float64
	Metrics         core.Metrics
	ExpectancyR     float64
	ExpectancyRub   float64
	ProfitFactor    float64
	TickerBusySkips int
	Trades          []models.ClosedTrade
	ByExperiment    map[string]ExperimentTradeStats
}

// ExperimentTradeStats — разбивка сделок по experiment id.
type ExperimentTradeStats struct {
	Trades      int
	NetPnL      float64
	ExpectancyR float64
	WinRate     float64
}

// PortfolioBacktestOptions — параметры прогона portfolio-backtest.
type PortfolioBacktestOptions struct {
	ConfigPath  string
	HistoryDir  string
	Deposit     float64 // 0 = из первого experiment / 200k
	MaxParallel int     // 0 = 5
	From        time.Time
	To          time.Time
}

// RunPortfolioBacktest прогоняет все experiments из bot YAML на одном депозите
// с общим GlobalRisk и one-position-per-ticker.
func RunPortfolioBacktest(ctx context.Context, opts PortfolioBacktestOptions) (PortfolioBacktestResult, error) {
	cfg, err := config.Load(opts.ConfigPath)
	if err != nil {
		return PortfolioBacktestResult{}, err
	}
	experiments := cfg.ResolvedExperiments()
	if len(experiments) == 0 {
		return PortfolioBacktestResult{}, fmt.Errorf("portfolio-backtest: нет experiments в %s", opts.ConfigPath)
	}

	accountRisk := cfg.AccountRisk()
	deposit := opts.Deposit
	if deposit <= 0 {
		deposit = accountRisk.Deposit
	}
	if deposit <= 0 {
		deposit = 200_000
	}
	maxParallel := opts.MaxParallel
	if maxParallel <= 0 {
		maxParallel = 5
	}
	dailyLossPct := accountRisk.MaxDailyLossPercent
	if dailyLossPct <= 0 {
		dailyLossPct = 2.0
	}
	maxDailyLoss := deposit * dailyLossPct / 100
	costsCfg := cfg.CostsConfig()
	riskPerTrade := accountRisk.RiskPerTradePercent
	if riskPerTrade <= 0 {
		riskPerTrade = 0.5
	}

	tickers := cfg.AllTickerSymbols()
	candleData, err := LoadCandleData(opts.HistoryDir, tickers)
	if err != nil {
		return PortfolioBacktestResult{}, err
	}

	from, to := opts.From, opts.To
	if from.IsZero() || to.IsZero() {
		dataFrom, dataTo, ok := marketdata.CandleDataRange(candleData)
		if !ok {
			return PortfolioBacktestResult{}, fmt.Errorf("portfolio-backtest: пустая история")
		}
		if from.IsZero() {
			from = dataFrom
		}
		if to.IsZero() {
			to = dataTo
		}
	}

	candlesByTicker := candlesByTickerInRange(candleData, tickers, from, to)
	if len(candlesByTicker) == 0 {
		return PortfolioBacktestResult{}, fmt.Errorf("portfolio-backtest: нет свечей в периоде %s…%s",
			from.Format("2006-01-02"), to.Format("2006-01-02"))
	}

	runnerCfgs := make(map[string]backtest.RunnerConfig)
	for _, exp := range experiments {
		session := cfg.SessionForExperiment(exp)
		trailCfg := exp.Strategy.TrailingConfig(1.0, costsCfg, cfg.ClassCode)

		for _, tc := range cfg.TickersForExperiment(exp) {
			step := tc.StepPriceValue
			if step <= 0 {
				step = 1.0
			}
			slotKey := exp.ID + "/" + tc.Symbol
			rcStrat, err := exp.Strategy.BuildStrategy(session)
			if err != nil {
				return PortfolioBacktestResult{}, fmt.Errorf("%s: %w", slotKey, err)
			}
			slotTrail := trailCfg
			slotTrail.StepPriceValue = step
			runnerCfgs[slotKey] = backtest.RunnerConfig{
				Ticker:          tc.Symbol,
				ClassCode:       cfg.ClassCode,
				CandleTimeframe: cfg.CandleTimeFrame,
				TradingMode:     config.TradingModeVirtual,
				RunID:           "portfolio-backtest",
				ExperimentID:    exp.ID,
				StepPriceValue:  step,
				Deposit:         deposit,
				MaxDailyLoss:    maxDailyLoss,
				RiskPerTradePct: riskPerTrade,
				MaxTradesPerDay: exp.Strategy.MaxTradesPerTickerPerDay,
				Strategy:        rcStrat,
				StrategyID:      exp.Strategy.TypeOrDefault(),
				StopMode:        exp.Strategy.StopMode,
				Lookback:        exp.Strategy.Lookback,
				TrailCfg:        slotTrail,
				RewardRatio:     exp.Strategy.EffectiveRewardRatio(),
				SessionCfg:      session,
			}
		}
	}

	store := memory.NewTradeStore()
	executor := execution.NewVirtualExecutor(deposit)
	globalRisk := risk.NewGlobalRiskController(deposit, dailyLossPct, riskPerTrade, maxParallel)

	portfolio, err := backtest.NewPortfolioRunner(backtest.PortfolioRunnerConfig{
		Tickers:    runnerCfgs,
		SessionCfg: cfg.Session,
		GlobalRisk: globalRisk,
	}, store)
	if err != nil {
		return PortfolioBacktestResult{}, err
	}

	if err := portfolio.Run(ctx, candlesByTicker, executor); err != nil {
		return PortfolioBacktestResult{}, err
	}

	trades := store.Trades()
	metrics := AggregateTrades(trades, costsCfg, cfg.ClassCode)
	expR, expRub, pf := detailedTradeStats(trades, costsCfg, cfg.ClassCode)
	byExp := statsByExperiment(trades, costsCfg, cfg.ClassCode)

	return PortfolioBacktestResult{
		From:            from,
		To:              to,
		Deposit:         deposit,
		Metrics:         metrics,
		ExpectancyR:     expR,
		ExpectancyRub:   expRub,
		ProfitFactor:    pf,
		TickerBusySkips: portfolio.TickerBusySkips,
		Trades:          trades,
		ByExperiment:    byExp,
	}, nil
}

func detailedTradeStats(trades []models.ClosedTrade, costsCfg costs.Config, classCode string) (expR, expRub, pf float64) {
	if len(trades) == 0 {
		return 0, 0, 0
	}
	var sumR, sumNet, grossWin, grossLoss float64
	for _, t := range trades {
		net := core.NetPnLFromTrade(t, costsCfg, classCode)
		sumNet += net
		step := t.StepPriceValue
		if step <= 0 {
			step = 1
		}
		riskAmt := t.RDistance * float64(t.Quantity) * step
		if riskAmt > 0 {
			sumR += net / riskAmt
		} else {
			sumR += t.PnLR
		}
		if net > 0 {
			grossWin += net
		} else if net < 0 {
			grossLoss += -net
		}
	}
	n := float64(len(trades))
	expR = sumR / n
	expRub = sumNet / n
	if grossLoss > 0 {
		pf = grossWin / grossLoss
	} else if grossWin > 0 {
		pf = 99
	}
	return expR, expRub, pf
}

func statsByExperiment(trades []models.ClosedTrade, costsCfg costs.Config, classCode string) map[string]ExperimentTradeStats {
	type acc struct {
		n, wins   int
		net, sumR float64
	}
	by := make(map[string]*acc)
	for _, t := range trades {
		id := t.ExperimentID
		if id == "" {
			id = "default"
		}
		a := by[id]
		if a == nil {
			a = &acc{}
			by[id] = a
		}
		net := core.NetPnLFromTrade(t, costsCfg, classCode)
		a.n++
		a.net += net
		if net > 0 {
			a.wins++
		}
		step := t.StepPriceValue
		if step <= 0 {
			step = 1
		}
		riskAmt := t.RDistance * float64(t.Quantity) * step
		if riskAmt > 0 {
			a.sumR += net / riskAmt
		} else {
			a.sumR += t.PnLR
		}
	}
	out := make(map[string]ExperimentTradeStats, len(by))
	for id, a := range by {
		s := ExperimentTradeStats{Trades: a.n, NetPnL: a.net}
		if a.n > 0 {
			s.ExpectancyR = a.sumR / float64(a.n)
			s.WinRate = float64(a.wins) / float64(a.n)
		}
		out[id] = s
	}
	return out
}
