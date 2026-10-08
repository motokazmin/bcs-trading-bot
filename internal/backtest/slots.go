package backtest

import (
	"context"
	"sort"

	"bcs-trading-bot/internal/config"
	"bcs-trading-bot/internal/engine/costs"
	"bcs-trading-bot/internal/engine/execution"
	"bcs-trading-bot/internal/engine/risk"
	"bcs-trading-bot/internal/models"
	"bcs-trading-bot/internal/strategy"
)

// Account — счёт портфельного прогона: общий для portfolio-backtest и прогрева live.
type Account struct {
	Deposit            float64
	MaxDailyLoss       float64
	RiskPerTradePct    float64
	CashUtilizationPct float64
	Costs              costs.Config
}

// NewStrategyFunc создаёт (или отдаёт готовую) сигнальную стратегию слота.
type NewStrategyFunc func(slotKey string, exp config.ResolvedExperiment, session config.SessionConfig) (strategy.CandleStrategy, error)

// SlotKey — ключ слота портфеля: "experimentID/ticker".
func SlotKey(experimentID, ticker string) string { return experimentID + "/" + ticker }

// SlotConfigs собирает RunnerConfig всех слотов (эксперимент × тикер). Один сборщик на
// portfolio-backtest и прогрев live: разъедутся параметры слота — прогретая стратегия
// окажется не в том состоянии, в каком её держит backtest.
func SlotConfigs(cfg *config.Config, experiments []config.ResolvedExperiment, acc Account, runID string, newStrategy NewStrategyFunc) (map[string]RunnerConfig, error) {
	out := make(map[string]RunnerConfig)
	for _, exp := range experiments {
		session := cfg.SessionForExperiment(exp)
		trailCfg := exp.Strategy.TrailingConfig(1.0, acc.Costs, cfg.ClassCode)

		for _, tc := range cfg.TickersForExperiment(exp) {
			step := tc.StepPriceValue
			if step <= 0 {
				step = 1.0
			}
			slotKey := SlotKey(exp.ID, tc.Symbol)
			strat, err := newStrategy(slotKey, exp, session)
			if err != nil {
				return nil, err
			}
			slotTrail := trailCfg
			slotTrail.StepPriceValue = step
			out[slotKey] = RunnerConfig{
				CostsCfg:           acc.Costs,
				Ticker:             tc.Symbol,
				ClassCode:          cfg.ClassCode,
				CandleTimeframe:    exp.CandleTimeframe,
				TradingMode:        config.TradingModeVirtual,
				RunID:              runID,
				ExperimentID:       exp.ID,
				StepPriceValue:     step,
				Deposit:            acc.Deposit,
				MaxDailyLoss:       acc.MaxDailyLoss,
				RiskPerTradePct:    acc.RiskPerTradePct,
				CashUtilizationPct: acc.CashUtilizationPct,
				MaxTradesPerDay:    exp.Strategy.MaxTradesPerTickerPerDay,
				Strategy:           strat,
				StrategyID:         exp.Strategy.TypeOrDefault(),
				StopMode:           exp.Strategy.StopMode,
				Lookback:           exp.Strategy.Lookback,
				TrailCfg:           slotTrail,
				RewardRatio:        exp.Strategy.EffectiveRewardRatio(),
				SessionCfg:         session,
			}
		}
	}
	return out, nil
}

// Warmup прогоняет историю через стратегии слотов портфельным раннером — ровно как
// portfolio-backtest — и выбрасывает сделки. После него стратегии в том состоянии,
// в каком их держал бы backtest к концу истории: буфер видел только бары с разрешённым
// входом и без позиции. Сделки и кэш прогрева никуда не пишутся.
//
// Возвращает слоты, у которых к концу истории осталась открытая позиция: live стартует
// без неё и будет кормить такой слот барами, которых backtest стратегии не показал бы.
func Warmup(ctx context.Context, slots map[string]RunnerConfig, candles map[string][]models.Candle, global *risk.GlobalRiskController) ([]string, error) {
	p, err := NewPortfolioRunner(PortfolioRunnerConfig{Tickers: slots, GlobalRisk: global}, nil)
	if err != nil {
		return nil, err
	}
	deposit := 0.0
	for _, rc := range slots {
		deposit = rc.Deposit
		break
	}
	if err := p.Run(ctx, candles, execution.NewVirtualExecutor(deposit)); err != nil {
		return nil, err
	}
	return p.openSlots(), nil
}

func (p *PortfolioRunner) openSlots() []string {
	var out []string
	for key, st := range p.states {
		if st.position != nil {
			out = append(out, key)
		}
	}
	sort.Strings(out)
	return out
}
