package config

import (
	"fmt"
	"strings"

	"bcs-trading-bot/internal/costs"
	"bcs-trading-bot/internal/strategy"
	"bcs-trading-bot/internal/trailing"

	"gopkg.in/yaml.v3"
)

// StrategyConfig — параметры стратегии из YAML.
//
// Typed-поля читает engine (trailing, лимит входов, lookback в ClosedTrade).
// Остальные гиперпараметры стратегии живут в raw и пробрасываются в
// strategy.Params без правок этого файла на каждый новый ключ.
type StrategyConfig struct {
	Type                     string  `yaml:"type"`
	StopMode                 string  `yaml:"stop_mode"`
	Lookback                 int     `yaml:"lookback"`
	MaxTradesPerTickerPerDay int     `yaml:"max_trades_per_ticker_per_day"`
	TrailActivationR         float64 `yaml:"trail_activation_r"`
	TrailDiscreteStepR       float64 `yaml:"trail_discrete_step_r"`
	TrailStageMax            int     `yaml:"trail_stage_max"`
	TrailBreakevenR          float64 `yaml:"trail_breakeven_r"`

	raw map[string]interface{}
}

// yamlKeyToParam — snake_case YAML → camelCase strategy.Params (исключения из автоконвертации).
var yamlKeyToParam = map[string]string{
	"max_trades_per_ticker_per_day": "maxEntriesPerTickerPerDay",
}

// UnmarshalYAML сохраняет все ключи в raw и синхронизирует typed-поля.
func (s *StrategyConfig) UnmarshalYAML(value *yaml.Node) error {
	var raw map[string]interface{}
	if err := value.Decode(&raw); err != nil {
		return err
	}
	s.raw = raw
	s.syncTypedFromRaw()
	return nil
}

// MarshalYAML сериализует raw (или typed-поля, если raw пуст).
func (s StrategyConfig) MarshalYAML() (interface{}, error) {
	return s.YAMLMap(), nil
}

// YAMLMap — все поля стратегии для дампа best-config / отчётов.
func (s StrategyConfig) YAMLMap() map[string]interface{} {
	if len(s.raw) > 0 {
		out := make(map[string]interface{}, len(s.raw)+8)
		for k, v := range s.raw {
			out[k] = v
		}
		s.writeTypedInto(out)
		return out
	}
	out := map[string]interface{}{}
	s.writeTypedInto(out)
	return out
}

func (s StrategyConfig) writeTypedInto(out map[string]interface{}) {
	if s.Type != "" {
		out["type"] = s.Type
	}
	if s.StopMode != "" {
		out["stop_mode"] = s.StopMode
	}
	if s.Lookback > 0 {
		out["lookback"] = s.Lookback
	}
	if s.MaxTradesPerTickerPerDay > 0 {
		out["max_trades_per_ticker_per_day"] = s.MaxTradesPerTickerPerDay
	}
	if s.TrailActivationR > 0 {
		out["trail_activation_r"] = s.TrailActivationR
	}
	if s.TrailDiscreteStepR > 0 {
		out["trail_discrete_step_r"] = s.TrailDiscreteStepR
	}
	if s.TrailStageMax > 0 {
		out["trail_stage_max"] = s.TrailStageMax
	}
	if s.TrailBreakevenR > 0 {
		out["trail_breakeven_r"] = s.TrailBreakevenR
	}
}

func (s *StrategyConfig) syncTypedFromRaw() {
	if s.raw == nil {
		return
	}
	if v, ok := s.rawString("type"); ok {
		s.Type = v
	}
	if v, ok := s.rawString("stop_mode"); ok {
		s.StopMode = v
	}
	if v, ok := s.rawInt("lookback"); ok {
		s.Lookback = v
	}
	if v, ok := s.rawInt("max_trades_per_ticker_per_day"); ok {
		s.MaxTradesPerTickerPerDay = v
	}
	if v, ok := s.rawFloat("trail_activation_r"); ok {
		s.TrailActivationR = v
	}
	if v, ok := s.rawFloat("trail_discrete_step_r"); ok {
		s.TrailDiscreteStepR = v
	}
	if v, ok := s.rawInt("trail_stage_max"); ok {
		s.TrailStageMax = v
	}
	if v, ok := s.rawFloat("trail_breakeven_r"); ok {
		s.TrailBreakevenR = v
	}
}

// StrategyConfigFromFields собирает конфиг из snake_case map (optimizer → YAML).
func StrategyConfigFromFields(fields map[string]interface{}, stopMode string) StrategyConfig {
	raw := make(map[string]interface{}, len(fields)+1)
	for k, v := range fields {
		raw[k] = v
	}
	if stopMode != "" {
		if _, ok := raw["stop_mode"]; !ok {
			raw["stop_mode"] = stopMode
		}
	}
	cfg := StrategyConfig{raw: raw}
	cfg.syncTypedFromRaw()
	if cfg.StopMode == "" {
		cfg.StopMode = stopMode
	}
	return cfg
}

// TypeOrDefault возвращает type стратегии (default momentum_breakout).
func (s StrategyConfig) TypeOrDefault() string {
	t := strings.TrimSpace(s.Type)
	if t == "" {
		return strategy.DefaultType()
	}
	return t
}

// EffectiveRewardRatio — фактический R:R (с дефолтом по типу стратегии).
func (s StrategyConfig) EffectiveRewardRatio() float64 {
	if rr := s.Float("reward_ratio"); rr > 0 {
		return rr
	}
	return strategy.DefaultRewardRatio(s.TypeOrDefault())
}

func (s StrategyConfig) LongOnlyEnabled() bool {
	return s.Bool("long_only")
}

func (s StrategyConfig) VolumeFilterEnabled() bool {
	return s.Bool("volume_filter")
}

// Float читает float64 из raw (0 если нет).
func (s StrategyConfig) Float(key string) float64 {
	v, _ := s.rawFloat(key)
	return v
}

// Int читает int из raw (0 если нет).
func (s StrategyConfig) Int(key string) int {
	v, _ := s.rawInt(key)
	return v
}

// Bool читает bool из raw (false если нет).
func (s StrategyConfig) Bool(key string) bool {
	v, ok := s.rawBool(key)
	return ok && v
}

// BoolPtr читает *bool из raw (nil если ключа нет).
func (s StrategyConfig) BoolPtr(key string) *bool {
	v, ok := s.rawBool(key)
	if !ok {
		return nil
	}
	b := v
	return &b
}

// Удобные аксессоры для тестов и legacy call-sites (читают raw).
func (s StrategyConfig) ATRPeriod() int              { return s.Int("atr_period") }
func (s StrategyConfig) ATRMultiplier() float64      { return s.Float("atr_multiplier") }
func (s StrategyConfig) RewardRatio() float64        { return s.Float("reward_ratio") }
func (s StrategyConfig) BreakoutThreshold() float64  { return s.Float("breakout_threshold") }
func (s StrategyConfig) VolumeMinRatio() float64     { return s.Float("volume_min_ratio") }
func (s StrategyConfig) ORBMinutes() int             { return s.Int("orb_minutes") }
func (s StrategyConfig) FadeThreshold() float64      { return s.Float("fade_threshold") }
func (s StrategyConfig) FadeWindowMinutes() int      { return s.Int("fade_window_minutes") }
func (s StrategyConfig) FadeTradeEndMinutes() int    { return s.Int("fade_trade_end_minutes") }
func (s StrategyConfig) RequireInsideRange() *bool   { return s.BoolPtr("require_inside_range") }
func (s StrategyConfig) TrendSMAPeriod() int         { return s.Int("trend_sma_period") }
func (s StrategyConfig) StrategyEntryDelayMinutes() int {
	return s.Int("strategy_entry_delay_minutes")
}
func (s StrategyConfig) MinMinutesAboveVWAP() int    { return s.Int("min_minutes_above_vwap") }
func (s StrategyConfig) CompressionPercentile() float64 {
	return s.Float("compression_percentile")
}
func (s StrategyConfig) ATRBars() int            { return s.Int("atr_bars") }
func (s StrategyConfig) EntryStartMinutes() int  { return s.Int("entry_start_minutes") }
func (s StrategyConfig) EntryEndMinutes() int    { return s.Int("entry_end_minutes") }
func (s StrategyConfig) GapThreshold() float64   { return s.Float("gap_threshold") }
func (s StrategyConfig) RangeStartMinutes() int  { return s.Int("range_start_minutes") }
func (s StrategyConfig) RangeEndMinutes() int    { return s.Int("range_end_minutes") }
func (s StrategyConfig) RangeUseCap() *bool      { return s.BoolPtr("range_use_cap") }

// StrategyOptions конвертирует конфиг стратегии в параметры MomentumBreakout (legacy).
func (s StrategyConfig) StrategyOptions() strategy.Options {
	rangeUseCap := true
	if p := s.RangeUseCap(); p != nil {
		rangeUseCap = *p
	}
	return strategy.Options{
		Lookback:          s.Lookback,
		StopMode:          strings.ToLower(strings.TrimSpace(s.StopMode)),
		ATRPeriod:         s.ATRPeriod(),
		ATRMultiplier:     s.ATRMultiplier(),
		RewardRatio:       s.RewardRatio(),
		RangeUseCap:       rangeUseCap,
		VolumeFilter:      s.VolumeFilterEnabled(),
		VolumeMinRatio:    s.VolumeMinRatio(),
		BreakoutThreshold: s.BreakoutThreshold(),
	}
}

// TrailingConfig конвертирует параметры трейлинга из YAML в trailing.Config.
func (s StrategyConfig) TrailingConfig(stepPriceValue float64, costsCfg costs.Config, classCode string) trailing.Config {
	cfg := trailing.DefaultConfig()
	cfg.StepPriceValue = stepPriceValue
	cfg.Costs = costsCfg
	cfg.ClassCode = classCode
	if s.TrailActivationR > 0 {
		cfg.ActivationR = s.TrailActivationR
	}
	if s.TrailDiscreteStepR > 0 {
		cfg.DiscreteStepR = s.TrailDiscreteStepR
	}
	if s.TrailStageMax > 0 {
		cfg.StageMax = s.TrailStageMax
	}
	if s.TrailBreakevenR > 0 {
		cfg.BreakevenR = s.TrailBreakevenR
	}
	return cfg
}

func (s *StrategyConfig) applyDefaults() {
	if s.Lookback < 2 {
		s.Lookback = defaultLookback
		s.setRaw("lookback", s.Lookback)
	}
	s.StopMode = strings.ToLower(strings.TrimSpace(s.StopMode))
	if s.StopMode == "" {
		s.StopMode = strategy.StopModeRange
		s.setRaw("stop_mode", s.StopMode)
	}
	if s.ATRPeriod() < 2 {
		s.setRaw("atr_period", defaultATRPeriod)
	}
	if s.ATRMultiplier() <= 0 {
		s.setRaw("atr_multiplier", defaultATRMultiplier)
	}
}

func (s *StrategyConfig) setRaw(key string, val interface{}) {
	if s.raw == nil {
		s.raw = make(map[string]interface{})
	}
	s.raw[key] = val
}

func (s StrategyConfig) rawString(key string) (string, bool) {
	if s.raw == nil {
		return "", false
	}
	v, ok := s.raw[key]
	if !ok || v == nil {
		return "", false
	}
	switch x := v.(type) {
	case string:
		return x, true
	default:
		return fmt.Sprint(x), true
	}
}

func (s StrategyConfig) rawFloat(key string) (float64, bool) {
	if s.raw == nil {
		return 0, false
	}
	v, ok := s.raw[key]
	if !ok || v == nil {
		return 0, false
	}
	return toFloat64(v)
}

func (s StrategyConfig) rawInt(key string) (int, bool) {
	f, ok := s.rawFloat(key)
	if !ok {
		return 0, false
	}
	return int(f), true
}

func (s StrategyConfig) rawBool(key string) (bool, bool) {
	if s.raw == nil {
		return false, false
	}
	v, ok := s.raw[key]
	if !ok || v == nil {
		return false, false
	}
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

func toFloat64(v interface{}) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case float32:
		return float64(x), true
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	case uint64:
		return float64(x), true
	default:
		return 0, false
	}
}

func snakeToCamel(s string) string {
	parts := strings.Split(s, "_")
	if len(parts) == 1 {
		return s
	}
	var b strings.Builder
	b.WriteString(parts[0])
	for _, p := range parts[1:] {
		if p == "" {
			continue
		}
		b.WriteString(strings.ToUpper(p[:1]))
		if len(p) > 1 {
			b.WriteString(p[1:])
		}
	}
	return b.String()
}

func paramKeyForYAML(yamlKey string) string {
	if k, ok := yamlKeyToParam[yamlKey]; ok {
		return k
	}
	return snakeToCamel(yamlKey)
}
