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
	p := strategy.Params{}

	// Typed / composite-literal поля (тесты, ручная сборка).
	if s.Lookback > 0 {
		p["lookback"] = float64(s.Lookback)
	}
	if s.MaxTradesPerTickerPerDay > 0 {
		p["maxEntriesPerTickerPerDay"] = float64(s.MaxTradesPerTickerPerDay)
	}

	for key, val := range s.YAMLMap() {
		switch key {
		case "type", "stop_mode",
			"trail_activation_r", "trail_discrete_step_r", "trail_stage_max", "trail_breakeven_r":
			continue // stop_mode → BuildContext; trail_* → adapter (trailing.Apply), не сигнальные params
		}
		pk := paramKeyForYAML(key)
		switch key {
		case "volume_filter", "long_only", "require_inside_range", "allow_all_tickers":
			if b, ok := toBool(val); ok {
				if b {
					p[pk] = 1
				} else {
					p[pk] = 0
				}
			}
		case "range_use_cap":
			if b, ok := toBool(val); ok && !b {
				p[pk] = 0
			} else {
				p[pk] = 1
			}
		default:
			if f, ok := toFloat64(val); ok {
				p[pk] = f
			}
		}
	}

	if _, ok := p["rangeUseCap"]; !ok {
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

// ToParams — strategy.Params из YAML (bot BuildStrategy и offline tools).
func (s StrategyConfig) ToParams(session SessionConfig) (strategy.Params, strategy.BuildContext) {
	return s.toStrategyParams(session)
}

func toBool(v interface{}) (bool, bool) {
	switch x := v.(type) {
	case bool:
		return x, true
	case int:
		return x != 0, true
	case float64:
		return x != 0, true
	default:
		return false, false
	}
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
	return StrategyConfigFromFields(fields, stopMode)
}

// ValidateStrategyType проверяет type через registry.
func ValidateStrategyType(typeID string) error {
	return strategy.ValidateStrategyType(typeID)
}
