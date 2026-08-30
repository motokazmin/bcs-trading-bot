package strategy

import (
	"sync"

	"bcs-trading-bot/internal/models"
)

func init() {
	Register(Descriptor{
		ID:                 IDMeanReversion,
		DefaultSearchSpace: "configs/strategies/mean-reversion.yaml",
		NewFromParams:      newMeanReversionFromParams,
		ParamsToConfigFields: meanReversionConfigFields,
	})
}

// MeanReversion — fade от SMA при экстремальном отклонении.
type MeanReversion struct {
	mu sync.Mutex

	opts   meanReversionOpts
	buffer *candleBuffer
}

type meanReversionOpts struct {
	Lookback        int
	FadeThreshold   float64
	RewardRatio     float64
	StopMode        string
	ATRPeriod       int
	ATRMultiplier   float64
	RangeUseCap     bool
	VolumeFilter    bool
	VolumeMinRatio  float64
}

func (s *MeanReversion) ID() string { return IDMeanReversion }

func (s *MeanReversion) OnCandle(candle models.Candle) *models.Order {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.buffer.isDuplicateUpdate(candle) {
		return nil
	}
	s.buffer.push(candle)
	if len(s.buffer.history) < s.opts.Lookback {
		return nil
	}

	sma := calcSMA(s.buffer.history, s.opts.Lookback)
	if sma <= 0 {
		return nil
	}

	close := candle.Close
	deviation := (close - sma) / sma
	th := s.opts.FadeThreshold
	if th <= 0 {
		th = 0.01
	}

	var direction string
	switch {
	case deviation > th:
		direction = "SELL"
	case deviation < -th:
		direction = "BUY"
	default:
		return nil
	}

	if !passesVolumeFilter(s.buffer.history, candle, s.opts.VolumeFilter, s.opts.VolumeMinRatio) {
		return nil
	}

	upper, lower, _ := rangeLevels(s.buffer.history)
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

func newMeanReversionFromParams(params Params, ctx BuildContext) (CandleStrategy, error) {
	stopMode := ctx.StopMode
	if stopMode == "" {
		stopMode = StopModeATR
	}
	opts := meanReversionOpts{
		Lookback:       params.Int("lookback"),
		FadeThreshold:  params.Float("fadeThreshold"),
		RewardRatio:    params.Float("rewardRatio"),
		StopMode:       stopMode,
		ATRPeriod:      params.Int("atrPeriod"),
		ATRMultiplier:  params.Float("atrMultiplier"),
		RangeUseCap:    paramsBoolDefault(params, "rangeUseCap", true),
		VolumeFilter:   paramsBoolDefault(params, "volumeFilter", false),
		VolumeMinRatio: params.Float("volumeMinRatio"),
	}.normalized()
	if opts.VolumeMinRatio <= 0 {
		opts.VolumeMinRatio = params.Float("volumeFilterMultiplier")
	}
	return &MeanReversion{
		opts:   opts,
		buffer: newCandleBuffer(opts.Lookback),
	}, nil
}

func (o meanReversionOpts) normalized() meanReversionOpts {
	out := o
	if out.Lookback < 2 {
		out.Lookback = defaultLookback
	}
	if out.RewardRatio <= 0 {
		out.RewardRatio = 1.5
	}
	if out.StopMode == "" {
		out.StopMode = StopModeATR
	}
	if out.ATRPeriod < 2 {
		out.ATRPeriod = defaultATRPeriod
	}
	if out.ATRMultiplier <= 0 {
		out.ATRMultiplier = defaultATRMultiplier
	}
	return out
}

func meanReversionConfigFields(params Params, ctx BuildContext) map[string]interface{} {
	return map[string]interface{}{
		"lookback":                     params.Int("lookback"),
		"fade_threshold":               params.Float("fadeThreshold"),
		"reward_ratio":                 params.Float("rewardRatio"),
		"stop_mode":                    ctx.StopMode,
		"atr_period":                   params.Int("atrPeriod"),
		"atr_multiplier":               params.Float("atrMultiplier"),
		"max_trades_per_ticker_per_day": params.Int("maxEntriesPerTickerPerDay"),
	}
}
