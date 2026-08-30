package strategy

import (
	"sync"

	"bcs-trading-bot/internal/models"
)

func init() {
	Register(Descriptor{
		ID:                   IDMomentumSberDaytrend,
		DefaultSearchSpace:   "configs/strategies/momentum-sber-daytrend.yaml",
		NewFromParams:        newMomentumSberDaytrendFromParams,
		ParamsToConfigFields: momentumSberDaytrendConfigFields,
	})
}

// MomentumSberDaytrend — momentum_filtered + фильтр направления дневного тренда (только SBER).
type MomentumSberDaytrend struct {
	mu sync.Mutex

	opts    momentumSberDaytrendOpts
	buffer  *candleBuffer
	session SessionTimes

	day          string
	sessionOpen  float64
	prevDayClose float64
	lastClose    float64
	dayTrend     string // "up" / "down" / ""
}

type momentumSberDaytrendOpts struct {
	momentumBreakoutOpts
	LongOnly                  bool
	StrategyEntryDelayMinutes int
}

func (s *MomentumSberDaytrend) ID() string { return IDMomentumSberDaytrend }

func (s *MomentumSberDaytrend) OnCandle(candle models.Candle) *models.Order {
	s.mu.Lock()
	defer s.mu.Unlock()

	if candle.Ticker != "" && candle.Ticker != "SBER" {
		return nil
	}

	if s.buffer.isDuplicateUpdate(candle) {
		return nil
	}

	day := s.session.tradingDate(candle.Timestamp)
	if day != s.day {
		s.resetDay(day)
	}

	mins, ok := s.session.minutesSinceOpen(candle.Timestamp)
	if ok && mins >= 0 && s.sessionOpen <= 0 {
		s.sessionOpen = candle.Open
		if s.prevDayClose > 0 {
			if s.sessionOpen > s.prevDayClose {
				s.dayTrend = "up"
			} else if s.sessionOpen < s.prevDayClose {
				s.dayTrend = "down"
			}
		}
	}

	s.buffer.push(candle)
	s.lastClose = candle.Close

	if len(s.buffer.history) < s.opts.Lookback {
		return nil
	}

	entryDelay := s.opts.StrategyEntryDelayMinutes
	if entryDelay <= 0 {
		entryDelay = 150
	}
	if !ok || mins < entryDelay {
		return nil
	}

	if s.dayTrend == "" {
		return nil
	}

	upper, lower, okRange := rangeLevels(s.buffer.history)
	if !okRange {
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

	if direction == "BUY" && s.dayTrend != "up" {
		return nil
	}
	if direction == "SELL" && s.dayTrend != "down" {
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

func (s *MomentumSberDaytrend) resetDay(day string) {
	if s.lastClose > 0 {
		s.prevDayClose = s.lastClose
	}
	s.day = day
	s.sessionOpen = 0
	s.dayTrend = ""
}

func newMomentumSberDaytrendFromParams(params Params, ctx BuildContext) (CandleStrategy, error) {
	base := momentumBreakoutOptsFromParams(params, ctx)
	entryDelay := params.Int("strategyEntryDelayMinutes")
	if entryDelay <= 0 {
		entryDelay = 150
	}
	opts := momentumSberDaytrendOpts{
		momentumBreakoutOpts:      base,
		LongOnly:                  params.Bool("longOnly"),
		StrategyEntryDelayMinutes: entryDelay,
	}
	if opts.RewardRatio <= 0 {
		opts.RewardRatio = 2.0
	}
	opts.VolumeFilter = paramsBoolDefault(params, "volumeFilter", true)
	return &MomentumSberDaytrend{
		opts:    opts.normalized(),
		buffer:  newCandleBuffer(opts.Lookback),
		session: ctx.Session,
	}, nil
}

func (o momentumSberDaytrendOpts) normalized() momentumSberDaytrendOpts {
	out := o
	out.momentumBreakoutOpts = out.momentumBreakoutOpts.normalized()
	if out.StrategyEntryDelayMinutes <= 0 {
		out.StrategyEntryDelayMinutes = 150
	}
	if out.RewardRatio <= 0 {
		out.RewardRatio = 2.0
	}
	return out
}

func momentumSberDaytrendConfigFields(params Params, ctx BuildContext) map[string]interface{} {
	m := momentumBreakoutConfigFields(params, ctx)
	m["long_only"] = params.Bool("longOnly")
	entryDelay := params.Int("strategyEntryDelayMinutes")
	if entryDelay <= 0 {
		entryDelay = 150
	}
	m["strategy_entry_delay_minutes"] = entryDelay
	if rr := params.Float("rewardRatio"); rr > 0 {
		m["reward_ratio"] = rr
	}
	return m
}
