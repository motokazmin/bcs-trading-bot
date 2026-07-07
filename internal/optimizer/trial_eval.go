package optimizer

import (
	"context"
	"sort"

	"bcs-trading-bot/internal/bcs"
	"bcs-trading-bot/internal/config"
	"bcs-trading-bot/internal/simulation"
	"bcs-trading-bot/internal/storage/memory"
	"bcs-trading-bot/internal/strategy"
	"bcs-trading-bot/internal/trailing"
	"bcs-trading-bot/pkg/models"
)

// trialContext — параметры trial, вычисленные один раз на весь trial.
type trialContext struct {
	params      ParameterSet
	stratParams strategy.Params
	trailCfg    trailing.Config
	maxLoss     float64
	riskPct     float64
	maxTrades   int
	lookback    int
}

func (e *Evaluator) newTrialContext(params ParameterSet) trialContext {
	p := make(strategy.Params, len(params)+4)
	for k, v := range params {
		p[k] = v
	}
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
func (e *Evaluator) PrecomputeWindowSlices(windows []Window) {
	e.windowSlices = BuildWindowCandleSlices(windows, e.settings.Tickers, e.candleData)
}

func (e *Evaluator) evaluateTrial(ctx context.Context, index int, params ParameterSet, minTrades int) TrialResult {
	tc := e.newTrialContext(params)
	var scores []float64
	windowResults := make([]WindowResult, len(e.windowSlices))

	for i, slices := range e.windowSlices {
		pr := e.evaluateCandles(ctx, tc, slices.Candles)
		scores = append(scores, Score(pr.Metrics, minTrades))
		windowResults[i] = WindowResult{Metrics: pr.Metrics}
	}

	return TrialResult{
		Index:   index,
		Params:  params,
		Score:   MedianFloat(scores),
		Windows: windowResults,
	}
}

func (e *Evaluator) fixedValue(key string, fallback float64) float64 {
	if e.space != nil {
		return e.space.FixedValue(key, fallback)
	}
	return fallback
}

func (e *Evaluator) evaluateCandles(ctx context.Context, tc trialContext, candlesByTicker map[string][]models.Candle) PeriodResult {
	var trades []models.ClosedTrade

	tickers := make([]string, 0, len(candlesByTicker))
	for ticker := range candlesByTicker {
		tickers = append(tickers, ticker)
	}
	sort.Strings(tickers)

	for _, ticker := range tickers {
		filtered := candlesByTicker[ticker]
		if len(filtered) == 0 {
			continue
		}

		strat, err := strategy.NewFromParams(e.strategyID, tc.stratParams, e.buildCtx)
		if err != nil {
			continue
		}

		store := memory.NewTradeStore()
		executor := bcs.NewVirtualExecutor(e.settings.Deposit)

		runner, err := simulation.NewRunner(simulation.RunnerConfig{
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
		}, store)
		if err != nil {
			continue
		}

		_ = runner.Run(ctx, filtered, executor)
		trades = append(trades, store.Trades()...)
	}

	return PeriodResult{
		Metrics: AggregateTrades(trades, e.settings.CommissionPerLot),
		Trades:  trades,
	}
}
