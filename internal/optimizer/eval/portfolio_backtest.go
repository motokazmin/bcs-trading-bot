package eval

import (
	"context"
	"fmt"
	"sort"
	"time"

	"bcs-trading-bot/internal/config"
	"bcs-trading-bot/internal/engine/costs"
	"bcs-trading-bot/internal/engine/execution"
	"bcs-trading-bot/internal/engine/risk"
	"bcs-trading-bot/internal/engine/marketdata"
	"bcs-trading-bot/internal/engine/timeframe"
	"bcs-trading-bot/internal/models"
	core "bcs-trading-bot/internal/optimizer/core"
	"bcs-trading-bot/internal/strategy"
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
	// Skips — сигналы, не ставшие сделкой, по причине (backtest.Skip*); CashCapped —
	// открытые с объёмом, урезанным по кэшу.
	Skips      map[string]int
	CashCapped int
	Trades          []models.ClosedTrade
	// NetPnL — net по каждой сделке (после комиссии), параллельно Trades: в GrossPnL backtest
	// лежит валовый PnL, а комиссия считается здесь, по costs из YAML.
	NetPnL       []float64
	ByExperiment map[string]ExperimentTradeStats
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
	// SlippageBps — override проскальзывания из YAML (<0 = не переопределять).
	SlippageBps float64
	// Timeframe — override candle_timeframe всех experiments ("" = из YAML).
	Timeframe string
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
	if opts.SlippageBps >= 0 {
		costsCfg.SlippageBps = opts.SlippageBps
	}
	riskPerTrade := accountRisk.RiskPerTradePercent
	if riskPerTrade <= 0 {
		riskPerTrade = 0.5
	}

	if opts.Timeframe != "" {
		if _, err := timeframe.Duration(opts.Timeframe); err != nil {
			return PortfolioBacktestResult{}, fmt.Errorf("portfolio-backtest: %w", err)
		}
		for i := range experiments {
			experiments[i].CandleTimeframe = timeframe.Normalize(opts.Timeframe)
		}
	}
	candleData, err := loadPortfolioCandles(cfg, experiments, opts.HistoryDir)
	if err != nil {
		return PortfolioBacktestResult{}, err
	}
	tickers := cfg.AllTickerSymbols()

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

	runnerCfgs, err := backtest.SlotConfigs(cfg, experiments, backtest.Account{
		Deposit:            deposit,
		MaxDailyLoss:       maxDailyLoss,
		RiskPerTradePct:    riskPerTrade,
		CashUtilizationPct: accountRisk.EffectiveCashUtilization(),
		Costs:              costsCfg,
	}, "portfolio-backtest", func(slotKey string, exp config.ResolvedExperiment, session config.SessionConfig) (strategy.CandleStrategy, error) {
		strat, err := exp.Strategy.BuildStrategy(session)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", slotKey, err)
		}
		return strat, nil
	})
	if err != nil {
		return PortfolioBacktestResult{}, err
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
	netPnL := make([]float64, len(trades))
	for i, t := range trades {
		netPnL[i] = core.NetPnLFromTrade(t, costsCfg, cfg.ClassCode)
	}

	return PortfolioBacktestResult{
		From:            from,
		To:              to,
		Deposit:         deposit,
		Metrics:         metrics,
		ExpectancyR:     expR,
		ExpectancyRub:   expRub,
		ProfitFactor:    pf,
		Skips:           portfolio.Skips,
		CashCapped:      portfolio.CashCapped,
		Trades:          trades,
		NetPnL:          netPnL,
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

// loadPortfolioCandles загружает историю каждого тикера в таймфрейме его слотов.
// Портфельный раннер ведёт один поток баров на тикер, поэтому слоты одного тикера
// обязаны быть на одном таймфрейме; разные тикеры — могут на разных.
func loadPortfolioCandles(cfg *config.Config, experiments []config.ResolvedExperiment, historyDir string) (map[string][]models.Candle, error) {
	tfByTicker := make(map[string]string)
	for _, exp := range experiments {
		for _, tc := range cfg.TickersForExperiment(exp) {
			prev, seen := tfByTicker[tc.Symbol]
			if seen && prev != exp.CandleTimeframe {
				return nil, fmt.Errorf("portfolio-backtest: %s в слотах с разным таймфреймом (%s и %s у %s) — один поток баров на тикер",
					tc.Symbol, prev, exp.CandleTimeframe, exp.ID)
			}
			tfByTicker[tc.Symbol] = exp.CandleTimeframe
		}
	}
	byTF := make(map[string][]string)
	for ticker, tf := range tfByTicker {
		byTF[tf] = append(byTF[tf], ticker)
	}
	out := make(map[string][]models.Candle, len(tfByTicker))
	for tf, tickers := range byTF {
		sort.Strings(tickers)
		data, err := LoadCandleData(historyDir, tickers, tf)
		if err != nil {
			return nil, err
		}
		for ticker, candles := range data {
			out[ticker] = candles
		}
	}
	return out, nil
}
