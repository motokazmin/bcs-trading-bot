package optimizer

import (
	"context"

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
		maxLoss:     e.settings.Deposit * e.space.FixedValue("dailyLossLimitPercent", 2.0) / 100,
		riskPct:     e.space.FixedValue("riskPerTradePercent", 0.5),
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
	var trainScores, testScores []float64
	windowResults := make([]WindowResult, len(e.windowSlices))

	for i, slices := range e.windowSlices {
		trainM := e.evaluateCandles(ctx, tc, slices.Train)
		testM := e.evaluateCandles(ctx, tc, slices.Test)
		trainScores = append(trainScores, Score(trainM, minTrades))
		testScores = append(testScores, Score(testM, minTrades))
		windowResults[i] = WindowResult{Train: trainM, Test: testM}
	}

	return TrialResult{
		Index:      index,
		Params:     params,
		TrainScore: MeanFloat(trainScores),
		TestScore:  MedianFloat(testScores),
		Windows:    windowResults,
	}
}

func (e *Evaluator) evaluateCandles(ctx context.Context, tc trialContext, candlesByTicker map[string][]models.Candle) Metrics {
	var trades []models.ClosedTrade

	for _, ticker := range e.settings.Tickers {
		filtered, ok := candlesByTicker[ticker]
		if !ok || len(filtered) == 0 {
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

	return AggregateTrades(trades, e.settings.CommissionPerTrade)
}
