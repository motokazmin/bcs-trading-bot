package eval

import (
	"context"
	"fmt"
	"time"

	core "bcs-trading-bot/internal/optimizer/core"
	"bcs-trading-bot/internal/strategy"
)

// BacktestResult — метрики одного backtest-прогона.
type BacktestResult struct {
	StrategyID string    `json:"strategy_id"`
	From       time.Time `json:"from"`
	To         time.Time `json:"to"`
	Metrics    core.Metrics `json:"metrics"`
	NumTrades  int       `json:"num_trades"`
}

// RunBacktest прогоняет одну конфигурацию на периоде.
func RunBacktest(ctx context.Context, evaluator *Evaluator, from, to time.Time, params core.ParameterSet) BacktestResult {
	if params == nil {
		params = core.ParameterSet{}
	}
	m := evaluator.EvaluatePeriod(ctx, params, from, to)
	return BacktestResult{
		StrategyID: evaluator.strategyID,
		From:       from,
		To:         to,
		Metrics:    m,
		NumTrades:  m.NumTrades,
	}
}

// DefaultParamsFromSpace возвращает середину search space для smoke backtest.
func DefaultParamsFromSpace(space *core.SearchSpace) core.ParameterSet {
	if space == nil {
		return core.ParameterSet{}
	}
	out := make(core.ParameterSet, len(space.Parameters))
	for name, bounds := range space.Parameters {
		switch bounds.Type {
		case core.ParamInt:
			lo := int(bounds.Min)
			hi := int(bounds.Max)
			out[name] = float64((lo + hi) / 2)
		default:
			out[name] = (bounds.Min + bounds.Max) / 2
		}
	}
	return out
}

// ValidateSearchSpaceStrategy проверяет соответствие CLI strategy и YAML.
func ValidateSearchSpaceStrategy(cliStrategy string, space *core.SearchSpace) error {
	want := strategy.ResolveType(cliStrategy)
	got := strategy.ResolveType(space.Strategy)
	if got == "" {
		got = strategy.DefaultType()
	}
	if want != got {
		return fmt.Errorf("search-space strategy %q не совпадает с -strategy %q", got, want)
	}
	return strategy.ValidateStrategyType(want)
}
