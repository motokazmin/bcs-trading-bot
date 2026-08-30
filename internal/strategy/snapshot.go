package strategy

import (
	"encoding/json"
	"sort"

	"bcs-trading-bot/internal/engine/trailing"
)

// ParamsSnapshot — JSON-снимок параметров trial для привязки к сделке в backtest/export.
type ParamsSnapshot struct {
	StrategyType string             `json:"strategy_type"`
	StopMode     string             `json:"stop_mode"`
	Params       map[string]float64 `json:"params"`
	Trail        TrailSnapshot      `json:"trail"`
}

// TrailSnapshot — параметры трейлинга на момент trial.
type TrailSnapshot struct {
	ActivationR   float64 `json:"activation_r"`
	DiscreteStepR float64 `json:"discrete_step_r"`
	StageMax      int     `json:"stage_max"`
	BreakevenR    float64 `json:"breakeven_r,omitempty"`
}

// EncodeParamsSnapshot сериализует параметры trial в компактный JSON для ClosedTrade.
func EncodeParamsSnapshot(strategyType, stopMode string, params Params, trail trailing.Config) string {
	if strategyType == "" {
		strategyType = DefaultType()
	}
	out := ParamsSnapshot{
		StrategyType: strategyType,
		StopMode:     stopMode,
		Params:       paramsMapSorted(params),
		Trail: TrailSnapshot{
			ActivationR:   trail.ActivationR,
			DiscreteStepR: trail.DiscreteStepR,
			StageMax:      trail.StageMax,
			BreakevenR:    trail.BreakevenR,
		},
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return ""
	}
	return string(raw)
}

func paramsMapSorted(params Params) map[string]float64 {
	if len(params) == 0 {
		return nil
	}
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make(map[string]float64, len(params))
	for _, k := range keys {
		out[k] = params[k]
	}
	return out
}
