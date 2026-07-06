package config

import (
	"bcs-trading-bot/internal/strategy"
)

// BuildStrategy создаёт CandleStrategy из YAML-конфига.
func (s StrategyConfig) BuildStrategy(session SessionConfig) (strategy.CandleStrategy, error) {
	p, ctx := s.toStrategyParams(session)
	return strategy.NewFromParams(s.TypeOrDefault(), p, ctx)
}

func (s StrategyConfig) toStrategyParams(session SessionConfig) (strategy.Params, strategy.BuildContext) {
	p := strategy.Params{
		"lookback":                  float64(s.Lookback),
		"atrPeriod":                 float64(s.ATRPeriod),
		"atrMultiplier":             s.ATRMultiplier,
		"rewardRatio":               s.RewardRatio,
		"breakoutThreshold":         s.BreakoutThreshold,
		"volumeMinRatio":            s.VolumeMinRatio,
		"maxEntriesPerTickerPerDay": float64(s.MaxTradesPerTickerPerDay),
		"orbMinutes":                float64(s.ORBMinutes),
		"fadeThreshold":             s.FadeThreshold,
		"trendSMAPeriod":            float64(s.TrendSMAPeriod),
		"strategyEntryDelayMinutes": float64(s.StrategyEntryDelayMinutes),
	}
	if s.VolumeFilterEnabled() {
		p["volumeFilter"] = 1
	}
	if s.LongOnlyEnabled() {
		p["longOnly"] = 1
	}
	if s.RangeUseCap != nil && !*s.RangeUseCap {
		p["rangeUseCap"] = 0
	} else {
		p["rangeUseCap"] = 1
	}
	if p["volumeMinRatio"] == 0 && s.VolumeFilterEnabled() {
		p["volumeFilterMultiplier"] = defaultVolumeMinRatio
	}
	ctx := strategy.BuildContext{
		StopMode: s.StopMode,
		Session: strategy.SessionTimes{
			Timezone:          session.Timezone,
			SessionOpenTime:   session.SessionOpenTime,
			EntryDelayMinutes: session.EntryDelayMinutes,
		},
	}
	return p, ctx
}

const defaultVolumeMinRatio = 1.5

// StrategyConfigFromOptimizer собирает StrategyConfig из optimizer params.
func StrategyConfigFromOptimizer(strategyID string, params strategy.Params, stopMode string, session SessionConfig) StrategyConfig {
	id := strategy.ResolveType(strategyID)
	d, err := strategy.Get(id)
	if err != nil {
		return StrategyConfig{Type: id, StopMode: stopMode}
	}
	fields := map[string]interface{}{"type": id}
	if d.ParamsToConfigFields != nil {
		ctx := strategy.BuildContext{
			StopMode: stopMode,
			Session: strategy.SessionTimes{
				Timezone:          session.Timezone,
				SessionOpenTime:   session.SessionOpenTime,
				EntryDelayMinutes: session.EntryDelayMinutes,
			},
		}
		for k, v := range d.ParamsToConfigFields(params, ctx) {
			fields[k] = v
		}
	}
	return StrategyConfigFromMap(fields, stopMode)
}

// ValidateStrategyType проверяет type через registry.
func ValidateStrategyType(typeID string) error {
	return strategy.ValidateStrategyType(typeID)
}
