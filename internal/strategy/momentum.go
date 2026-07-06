package strategy

import (
	"sync"

	"bcs-trading-bot/pkg/models"
)

func init() {
	Register(Descriptor{
		ID:                 IDMomentumBreakout,
		DefaultSearchSpace: "config/optimizer/search-space-momentum.yaml",
		NewFromParams:      newMomentumBreakoutFromParams,
		ParamsToConfigFields: momentumBreakoutConfigFields,
	})
}

// MomentumBreakout — пробой локальных high/low за lookback.
type MomentumBreakout struct {
	mu sync.Mutex

	opts   momentumBreakoutOpts
	buffer *candleBuffer
}

type momentumBreakoutOpts struct {
	Lookback          int
	StopMode          string
	ATRPeriod         int
	ATRMultiplier     float64
	RewardRatio       float64
	RangeUseCap       bool
	VolumeFilter      bool
	VolumeMinRatio    float64
	BreakoutThreshold float64
}

// NewMomentumBreakout создаёт стратегию (legacy API для тестов).
func NewMomentumBreakout(opts Options) *MomentumBreakout {
	return newMomentumBreakout(momentumBreakoutOptsFromOptions(opts))
}

func newMomentumBreakout(opts momentumBreakoutOpts) *MomentumBreakout {
	opts = opts.normalized()
	return &MomentumBreakout{
		opts:   opts,
		buffer: newCandleBuffer(opts.Lookback),
	}
}

func (s *MomentumBreakout) ID() string { return IDMomentumBreakout }

func (s *MomentumBreakout) OnCandle(candle models.Candle) *models.Order {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.buffer.isDuplicateUpdate(candle) {
		return nil
	}
	s.buffer.push(candle)
	if len(s.buffer.history) < s.opts.Lookback {
		return nil
	}

	upper, lower, ok := rangeLevels(s.buffer.history)
	if !ok {
		return nil
	}

	close := candle.Close
	th := s.opts.BreakoutThreshold
	var direction string
	switch {
	case close > upper*(1+th):
		direction = "BUY"
	case close < lower*(1-th):
		direction = "SELL"
	default:
		return nil
	}

	if !passesVolumeFilter(s.buffer.history, candle, s.opts.VolumeFilter, s.opts.VolumeMinRatio) {
		return nil
	}

	entry := close
	stopCfg := stopConfig{
		StopMode: s.opts.StopMode, ATRPeriod: s.opts.ATRPeriod,
		ATRMultiplier: s.opts.ATRMultiplier, RangeUseCap: s.opts.RangeUseCap,
		RewardRatio: s.opts.RewardRatio,
	}
	sl, tp := calcStopTP(direction, entry, upper, lower, s.buffer.history, stopCfg)
	order := buildOrder(candle, direction, entry, sl, tp, upper, lower)
	if order == nil {
		return nil
	}
	s.buffer.markSignal(candle)
	return order
}

func newMomentumBreakoutFromParams(params Params, ctx BuildContext) (CandleStrategy, error) {
	return newMomentumBreakout(momentumBreakoutOptsFromParams(params, ctx)), nil
}

func momentumBreakoutOptsFromParams(params Params, ctx BuildContext) momentumBreakoutOpts {
	stopMode := ctx.StopMode
	if stopMode == "" {
		stopMode = StopModeRange
	}
	volFilter := true
	if _, ok := params["volumeFilter"]; ok {
		volFilter = params.Bool("volumeFilter")
	} else if _, ok := params["volumeFilterMultiplier"]; ok {
		volFilter = true
	}
	volMin := params.Float("volumeMinRatio")
	if volMin <= 0 {
		volMin = params.Float("volumeFilterMultiplier")
	}
	return momentumBreakoutOpts{
		Lookback:          params.Int("lookback"),
		StopMode:          stopMode,
		ATRPeriod:         params.Int("atrPeriod"),
		ATRMultiplier:     params.Float("atrMultiplier"),
		RewardRatio:       params.Float("rewardRatio"),
		RangeUseCap:       !paramsBoolDefault(params, "rangeUseCap", true),
		VolumeFilter:      volFilter,
		VolumeMinRatio:    volMin,
		BreakoutThreshold: params.Float("breakoutThreshold"),
	}.normalized()
}

func momentumBreakoutOptsFromOptions(o Options) momentumBreakoutOpts {
	n := o.normalized()
	return momentumBreakoutOpts{
		Lookback: n.Lookback, StopMode: n.StopMode, ATRPeriod: n.ATRPeriod,
		ATRMultiplier: n.ATRMultiplier, RewardRatio: n.RewardRatio,
		RangeUseCap: n.RangeUseCap, VolumeFilter: n.VolumeFilter,
		VolumeMinRatio: n.VolumeMinRatio, BreakoutThreshold: n.BreakoutThreshold,
	}.normalized()
}

func (o momentumBreakoutOpts) normalized() momentumBreakoutOpts {
	out := o
	if out.Lookback < 2 {
		out.Lookback = defaultLookback
	}
	if out.StopMode == "" {
		out.StopMode = StopModeRange
	}
	if out.ATRPeriod < 2 {
		out.ATRPeriod = defaultATRPeriod
	}
	if out.ATRMultiplier <= 0 {
		out.ATRMultiplier = defaultATRMultiplier
	}
	if out.RewardRatio <= 0 {
		out.RewardRatio = defaultRiskRewardRatio
	}
	if out.VolumeMinRatio <= 0 {
		out.VolumeMinRatio = defaultVolumeMinRatio
	}
	return out
}

func momentumBreakoutConfigFields(params Params, ctx BuildContext) map[string]interface{} {
	vol := params.Bool("volumeFilter")
	if _, ok := params["volumeFilter"]; !ok {
		vol = params.Float("volumeFilterMultiplier") > 0
	}
	volMin := params.Float("volumeMinRatio")
	if volMin <= 0 {
		volMin = params.Float("volumeFilterMultiplier")
	}
	return map[string]interface{}{
		"lookback":                     params.Int("lookback"),
		"stop_mode":                    ctx.StopMode,
		"atr_period":                   params.Int("atrPeriod"),
		"atr_multiplier":               params.Float("atrMultiplier"),
		"reward_ratio":                 params.Float("rewardRatio"),
		"breakout_threshold":           params.Float("breakoutThreshold"),
		"volume_filter":                vol,
		"volume_min_ratio":             volMin,
		"max_trades_per_ticker_per_day": params.Int("maxEntriesPerTickerPerDay"),
	}
}

func paramsBoolDefault(p Params, key string, def bool) bool {
	if _, ok := p[key]; !ok {
		return def
	}
	return p.Bool(key)
}
