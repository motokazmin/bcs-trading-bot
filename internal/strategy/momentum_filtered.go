package strategy

import (
	"sync"

	"bcs-trading-bot/pkg/models"
)

func init() {
	Register(Descriptor{
		ID:                 IDMomentumFiltered,
		DefaultSearchSpace: "configs/strategies/momentum-filtered.yaml",
		NewFromParams:      newMomentumFilteredFromParams,
		ParamsToConfigFields: momentumFilteredConfigFields,
	})
}

// MomentumFiltered — momentum breakout с фильтрами режима и настраиваемым RR.
type MomentumFiltered struct {
	mu sync.Mutex

	opts    momentumFilteredOpts
	buffer  *candleBuffer
	session SessionTimes
	trend   *TrendProvider
}

type momentumFilteredOpts struct {
	momentumBreakoutOpts
	LongOnly                  bool
	TrendSMAPeriod            int
	StrategyEntryDelayMinutes int
	TrendGate                 TrendGateOpts
}

func (s *MomentumFiltered) ID() string { return IDMomentumFiltered }

func (s *MomentumFiltered) OnCandle(candle models.Candle) *models.Order {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.buffer.isDuplicateUpdate(candle) {
		return nil
	}
	s.buffer.push(candle)
	if s.trend != nil {
		s.trend.Observe(candle)
	}
	if len(s.buffer.history) < s.opts.Lookback {
		return nil
	}

	if s.opts.StrategyEntryDelayMinutes > 0 {
		if mins, ok := s.session.minutesSinceOpen(candle.Timestamp); !ok || mins < s.opts.StrategyEntryDelayMinutes {
			return nil
		}
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
		if s.opts.LongOnly {
			return nil
		}
		direction = "SELL"
	default:
		return nil
	}

	if s.opts.TrendSMAPeriod > 0 {
		sma := calcSMA(s.buffer.history, s.opts.TrendSMAPeriod)
		if sma <= 0 {
			return nil
		}
		if direction == "BUY" && close <= sma {
			return nil
		}
		if direction == "SELL" && close >= sma {
			return nil
		}
	}

	if !passesVolumeFilter(s.buffer.history, candle, s.opts.VolumeFilter, s.opts.VolumeMinRatio) {
		return nil
	}

	allow, widen := applyTrendGate(s.opts.TrendGate, s.trend, candle.Ticker, direction)
	if !allow {
		return nil
	}

	entry := close
	stopCfg := stopConfig{
		StopMode: s.opts.StopMode, ATRPeriod: s.opts.ATRPeriod,
		ATRMultiplier: s.opts.ATRMultiplier, RangeUseCap: s.opts.RangeUseCap,
		RewardRatio: s.opts.RewardRatio,
	}
	sl, tp := calcStopTP(direction, entry, upper, lower, s.buffer.history, stopCfg)
	if widen {
		sl, tp = widenStopTP(direction, entry, sl, tp, s.opts.TrendGate.AgainstMultiplier)
	}
	order := buildOrder(candle, direction, entry, sl, tp, upper, lower)
	if order == nil {
		return nil
	}
	s.buffer.markSignal(candle)
	return order
}

func newMomentumFilteredFromParams(params Params, ctx BuildContext) (CandleStrategy, error) {
	base := momentumBreakoutOptsFromParams(params, ctx)
	opts := momentumFilteredOpts{
		momentumBreakoutOpts:      base,
		LongOnly:                  params.Bool("longOnly"),
		TrendSMAPeriod:            params.Int("trendSMAPeriod"),
		StrategyEntryDelayMinutes: params.Int("strategyEntryDelayMinutes"),
		TrendGate:                 trendGateOptsFromParams(params),
	}
	if opts.RewardRatio <= 0 {
		opts.RewardRatio = params.Float("rewardRatio")
	}
	if opts.RewardRatio <= 0 {
		opts.RewardRatio = 2.0
	}
	volFilter := paramsBoolDefault(params, "volumeFilter", true)
	opts.VolumeFilter = volFilter
	opts = opts.normalized()
	s := &MomentumFiltered{
		opts:    opts,
		buffer:  newCandleBuffer(opts.Lookback),
		session: ctx.Session,
	}
	if opts.TrendGate.Enabled {
		s.trend = NewTrendProvider(opts.TrendGate.SlowPeriod * 3)
	}
	return s, nil
}

func (o momentumFilteredOpts) normalized() momentumFilteredOpts {
	out := o
	out.momentumBreakoutOpts = out.momentumBreakoutOpts.normalized()
	out.TrendGate = out.TrendGate.normalized()
	return out
}

func momentumFilteredConfigFields(params Params, ctx BuildContext) map[string]interface{} {
	m := momentumBreakoutConfigFields(params, ctx)
	m["long_only"] = params.Bool("longOnly")
	m["trend_sma_period"] = params.Int("trendSMAPeriod")
	m["strategy_entry_delay_minutes"] = params.Int("strategyEntryDelayMinutes")
	if rr := params.Float("rewardRatio"); rr > 0 {
		m["reward_ratio"] = rr
	}
	mergeTrendGateConfigFields(m, params)
	return m
}
