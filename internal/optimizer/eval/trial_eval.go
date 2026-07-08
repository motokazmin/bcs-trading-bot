package eval

import (
	"context"
	"sort"

	"bcs-trading-bot/internal/bcs"
	"bcs-trading-bot/internal/config"
	core "bcs-trading-bot/internal/optimizer/core"
	"bcs-trading-bot/internal/risk"
	"bcs-trading-bot/internal/simulation"
	"bcs-trading-bot/internal/storage/memory"
	"bcs-trading-bot/internal/strategy"
	"bcs-trading-bot/internal/trailing"
	"bcs-trading-bot/pkg/models"
)

// trialContext — параметры trial, вычисленные один раз на весь trial.
type trialContext struct {
	params      core.ParameterSet
	stratParams strategy.Params
	trailCfg    trailing.Config
	maxLoss     float64
	riskPct     float64
	maxTrades   int
	lookback    int
}

func (e *Evaluator) newTrialContext(params core.ParameterSet) trialContext {
	// Копируем params в strategy.Params один раз на trial, чтобы не пересобирать
	// словарь на каждом окне walk-forward.
	p := make(strategy.Params, len(params)+4)
	for k, v := range params {
		p[k] = v
	}
	// Защита от нулевого ATR-периода в space: используем безопасный дефолт.
	if p["atrPeriod"] == 0 {
		p["atrPeriod"] = 14
	}
	return trialContext{
		params:      params,
		stratParams: p,
		trailCfg:    e.trailCfg(params),
		maxLoss:     e.settings.Deposit * e.fixedValue("dailyLossLimitPercent", 2.0) / 100,
		riskPct:     e.fixedValue("riskPerTradePercent", 0.5),
		maxTrades:   params.IntParam("maxEntriesPerTickerPerDay"),
		lookback:    params.IntParam("lookback"),
	}
}

// PrecomputeWindowSlices преднарезает свечи по окнам (вызывать после GenerateWindows).
func (e *Evaluator) PrecomputeWindowSlices(windows []core.Window) {
	e.windowSlices = BuildWindowCandleSlices(windows, e.settings.Tickers, e.candleData)
}

func (e *Evaluator) evaluateTrial(ctx context.Context, index int, params core.ParameterSet, minTrades int) TrialResult {
	tc := e.newTrialContext(params)
	var scores []float64
	windowResults := make([]WindowResult, len(e.windowSlices))

	// Один trial = одинаковые параметры на всех окнах; итоговый score — агрегат по окнам.
	for i, slices := range e.windowSlices {
		pr := e.evaluateCandles(ctx, tc, slices.Candles)
		scores = append(scores, core.Score(pr.Metrics, minTrades))
		windowResults[i] = WindowResult{Metrics: pr.Metrics}
	}

	return TrialResult{
		Index:   index,
		Params:  params,
		Score:   core.MedianFloat(scores),
		Windows: windowResults,
	}
}

// EvaluateTrialForCandidate оценивает один trial по предвычисленным окнам.
func (e *Evaluator) EvaluateTrialForCandidate(ctx context.Context, index int, params core.ParameterSet, minTrades int) TrialResult {
	return e.evaluateTrial(ctx, index, params, minTrades)
}

func (e *Evaluator) fixedValue(key string, fallback float64) float64 {
	if e.space != nil {
		return e.space.FixedValue(key, fallback)
	}
	return fallback
}

func (e *Evaluator) evaluateCandles(ctx context.Context, tc trialContext, candlesByTicker map[string][]models.Candle) PeriodResult {
	var trades []models.ClosedTrade

	// Фиксируем стабильный порядок тикеров: одинаковый вход -> одинаковый порядок обхода.
	tickers := make([]string, 0, len(candlesByTicker))
	for ticker := range candlesByTicker {
		tickers = append(tickers, ticker)
	}
	sort.Strings(tickers)

	if len(tickers) == 0 {
		return PeriodResult{}
	}

	store := memory.NewTradeStore()
	// Optimizer всегда считает в virtual-режиме, без реальных ордеров.
	executor := bcs.NewVirtualExecutor(e.settings.Deposit)

	maxParallel := 2
	if e.space != nil {
		if v := e.space.FixedValue("maxParallelTrades", 0); v > 0 {
			maxParallel = int(v)
		}
	}
	globalRisk := risk.NewGlobalRiskController(
		e.settings.Deposit,
		e.fixedValue("dailyLossLimitPercent", 2.0),
		maxParallel,
	)

	// Собираем конфиги раннеров по тикерам для портфельного прогона в одном окне.
	runnerCfgs := make(map[string]simulation.RunnerConfig, len(tickers))
	for _, ticker := range tickers {
		filtered := candlesByTicker[ticker]
		if len(filtered) == 0 {
			continue
		}

		strat, err := strategy.NewFromParams(e.strategyID, tc.stratParams, e.buildCtx)
		if err != nil {
			continue
		}

		runnerCfgs[ticker] = simulation.RunnerConfig{
			Ticker:          ticker,
			ClassCode:       e.settings.ClassCode,
			CandleTimeframe: e.settings.CandleTimeframe,
			TradingMode:     config.TradingModeVirtual,
			RunID:           "optimizer",
			ExperimentID:    "optimizer",
			StepPriceValue:  e.settings.StepPriceValue,
			Deposit:         e.settings.Deposit,
			MaxDailyLoss:    tc.maxLoss,
			RiskPerTradePct: tc.riskPct,
			MaxTradesPerDay: tc.maxTrades,
			Strategy:        strat,
			StrategyID:      e.strategyID,
			StopMode:        e.settings.StopMode,
			Lookback:        tc.lookback,
			TrailCfg:        tc.trailCfg,
			SessionCfg:      e.settings.Session,
		}
	}

	if len(runnerCfgs) == 0 {
		return PeriodResult{}
	}

	// Портфельный раннер учитывает общий риск и конкурентные позиции между тикерами.
	portfolio, err := simulation.NewPortfolioRunner(simulation.PortfolioRunnerConfig{
		Tickers:    runnerCfgs,
		SessionCfg: e.settings.Session,
		GlobalRisk: globalRisk,
	}, store)
	if err != nil {
		return PeriodResult{}
	}

	_ = portfolio.Run(ctx, candlesByTicker, executor)
	trades = append(trades, store.Trades()...)

	// Агрегируем сделки портфеля в единые метрики периода (после сортировки в AggregateTrades).
	return PeriodResult{
		Metrics: AggregateTrades(trades, e.settings.CommissionPerLot),
		Trades:  trades,
	}
}
