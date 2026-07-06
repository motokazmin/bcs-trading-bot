package strategy

import (
	"sync"

	"bcs-trading-bot/pkg/models"
)

func init() {
	Register(Descriptor{
		ID:                 IDOpeningRange,
		DefaultSearchSpace: "config/optimizer/search-space-orb.yaml",
		NewFromParams:      newOpeningRangeFromParams,
		ParamsToConfigFields: openingRangeConfigFields,
	})
}

// OpeningRange — пробой диапазона первых N минут сессии (ORB).
type OpeningRange struct {
	mu sync.Mutex

	opts    openingRangeOpts
	buffer  *candleBuffer
	session SessionTimes

	day         string
	orbHigh     float64
	orbLow      float64
	orbComplete bool
	inORB       bool
}

type openingRangeOpts struct {
	ORBMinutes        int
	BreakoutThreshold float64
	StopMode          string
	ATRPeriod         int
	ATRMultiplier     float64
	RewardRatio       float64
	RangeUseCap       bool
}

func (s *OpeningRange) ID() string { return IDOpeningRange }

func (s *OpeningRange) OnCandle(candle models.Candle) *models.Order {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.buffer.isDuplicateUpdate(candle) {
		return nil
	}

	day := s.session.tradingDate(candle.Timestamp)
	if day != s.day {
		s.day = day
		s.orbHigh = 0
		s.orbLow = 0
		s.orbComplete = false
		s.inORB = false
	}

	mins, ok := s.session.minutesSinceOpen(candle.Timestamp)
	if !ok {
		s.buffer.push(candle)
		return nil
	}

	if mins < 0 {
		s.buffer.push(candle)
		return nil
	}

	if !s.orbComplete && mins < s.opts.ORBMinutes {
		s.inORB = true
		if s.orbHigh == 0 && s.orbLow == 0 {
			s.orbHigh = candle.High
			s.orbLow = candle.Low
		} else {
			if candle.High > s.orbHigh {
				s.orbHigh = candle.High
			}
			if candle.Low < s.orbLow {
				s.orbLow = candle.Low
			}
		}
		s.buffer.push(candle)
		return nil
	}

	if !s.orbComplete && s.inORB {
		s.orbComplete = true
		s.inORB = false
	}

	s.buffer.push(candle)
	if !s.orbComplete || s.orbHigh <= s.orbLow {
		return nil
	}

	close := candle.Close
	th := s.opts.BreakoutThreshold
	var direction string
	switch {
	case close > s.orbHigh*(1+th):
		direction = "BUY"
	case close < s.orbLow*(1-th):
		direction = "SELL"
	default:
		return nil
	}

	entry := close
	upper, lower := s.orbHigh, s.orbLow
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

func newOpeningRangeFromParams(params Params, ctx BuildContext) (CandleStrategy, error) {
	stopMode := ctx.StopMode
	if stopMode == "" {
		stopMode = StopModeATR
	}
	orbMin := params.Int("orbMinutes")
	if orbMin <= 0 {
		orbMin = 30
	}
	opts := openingRangeOpts{
		ORBMinutes:        orbMin,
		BreakoutThreshold: params.Float("breakoutThreshold"),
		StopMode:          stopMode,
		ATRPeriod:         params.Int("atrPeriod"),
		ATRMultiplier:     params.Float("atrMultiplier"),
		RewardRatio:       params.Float("rewardRatio"),
		RangeUseCap:       paramsBoolDefault(params, "rangeUseCap", true),
	}.normalized()
	return &OpeningRange{
		opts:    opts,
		buffer:  newCandleBuffer(50),
		session: ctx.Session,
	}, nil
}

func (o openingRangeOpts) normalized() openingRangeOpts {
	out := o
	if out.ORBMinutes <= 0 {
		out.ORBMinutes = 30
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
	if out.RewardRatio <= 0 {
		out.RewardRatio = 2.0
	}
	return out
}

func openingRangeConfigFields(params Params, ctx BuildContext) map[string]interface{} {
	return map[string]interface{}{
		"stop_mode":                    ctx.StopMode,
		"orb_minutes":                  params.Int("orbMinutes"),
		"breakout_threshold":           params.Float("breakoutThreshold"),
		"atr_period":                   params.Int("atrPeriod"),
		"atr_multiplier":               params.Float("atrMultiplier"),
		"reward_ratio":                 params.Float("rewardRatio"),
		"max_trades_per_ticker_per_day": params.Int("maxEntriesPerTickerPerDay"),
	}
}
