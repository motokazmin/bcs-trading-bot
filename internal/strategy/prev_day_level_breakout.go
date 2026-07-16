package strategy

import (
	"sync"

	"bcs-trading-bot/pkg/models"
)

func init() {
	Register(Descriptor{
		ID:                   IDPrevDayLevelBreakout,
		DefaultSearchSpace:   "configs/legacy/frequency-hypotheses/strategies/prev-day-level-breakout.yaml",
		NewFromParams:        newPrevDayLevelBreakoutFromParams,
		ParamsToConfigFields: prevDayLevelBreakoutConfigFields,
	})
}

// PrevDayLevelWhitelist — MGNT / ROSN / LKOH.
var PrevDayLevelWhitelist = map[string]struct{}{
	"MGNT": {},
	"ROSN": {},
	"LKOH": {},
}

// PrevDayLevelBreakout — пробой high/low предыдущего торгового дня.
type PrevDayLevelBreakout struct {
	mu sync.Mutex

	opts    prevDayLevelOpts
	buffer  *candleBuffer
	session SessionTimes

	day         string
	dayHigh     float64
	dayLow      float64
	prevDayHigh float64
	prevDayLow  float64
	hasPrevDay  bool
}

type prevDayLevelOpts struct {
	StrategyEntryDelayMinutes int
	EntryEndMinutes           int
	BreakoutThreshold         float64
	StopMode                  string
	ATRPeriod                 int
	ATRMultiplier             float64
	RewardRatio               float64
	RangeUseCap               bool
	VolumeFilter              bool
	VolumeMinRatio            float64
}

func (s *PrevDayLevelBreakout) ID() string { return IDPrevDayLevelBreakout }

func (s *PrevDayLevelBreakout) OnCandle(candle models.Candle) *models.Order {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !tickerAllowed(candle.Ticker, PrevDayLevelWhitelist, nil) {
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
	if !ok || mins < 0 {
		s.buffer.push(candle)
		return nil
	}

	s.updateDayRange(candle)
	s.buffer.push(candle)

	if !s.hasPrevDay || s.prevDayHigh <= s.prevDayLow {
		return nil
	}

	entryDelay := s.opts.StrategyEntryDelayMinutes
	if entryDelay <= 0 {
		entryDelay = 60
	}
	if mins < entryDelay {
		return nil
	}

	tradeEnd := s.opts.EntryEndMinutes
	if tradeEnd <= 0 {
		tradeEnd = 360
	}
	if mins > tradeEnd {
		return nil
	}

	if !passesVolumeFilter(s.buffer.history, candle, s.opts.VolumeFilter, s.opts.VolumeMinRatio) {
		return nil
	}

	close := candle.Close
	th := s.opts.BreakoutThreshold
	var direction string
	switch {
	case close > s.prevDayHigh*(1+th):
		direction = "BUY"
	case close < s.prevDayLow*(1-th):
		direction = "SELL"
	default:
		return nil
	}

	entry := close
	upper, lower := s.prevDayHigh, s.prevDayLow
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

func (s *PrevDayLevelBreakout) updateDayRange(candle models.Candle) {
	if s.dayHigh == 0 && s.dayLow == 0 {
		s.dayHigh = candle.High
		s.dayLow = candle.Low
		return
	}
	if candle.High > s.dayHigh {
		s.dayHigh = candle.High
	}
	if candle.Low < s.dayLow {
		s.dayLow = candle.Low
	}
}

func (s *PrevDayLevelBreakout) resetDay(day string) {
	if s.dayHigh > 0 && s.dayLow > 0 && s.dayHigh >= s.dayLow {
		s.prevDayHigh = s.dayHigh
		s.prevDayLow = s.dayLow
		s.hasPrevDay = true
	}
	s.day = day
	s.dayHigh = 0
	s.dayLow = 0
}

func newPrevDayLevelBreakoutFromParams(params Params, ctx BuildContext) (CandleStrategy, error) {
	stopMode := ctx.StopMode
	if stopMode == "" {
		stopMode = StopModeATR
	}
	entryDelay := params.Int("strategyEntryDelayMinutes")
	if entryDelay <= 0 {
		entryDelay = 60
	}
	tradeEnd := params.Int("entryEndMinutes")
	if tradeEnd <= 0 {
		tradeEnd = params.Int("fadeTradeEndMinutes")
	}
	if tradeEnd <= 0 {
		tradeEnd = 360
	}
	rewardRatio := params.Float("rewardRatio")
	if rewardRatio <= 0 {
		rewardRatio = 2.0
	}
	volMin := params.Float("volumeMinRatio")
	if volMin <= 0 {
		volMin = params.Float("volumeFilterMultiplier")
	}
	opts := prevDayLevelOpts{
		StrategyEntryDelayMinutes: entryDelay,
		EntryEndMinutes:           tradeEnd,
		BreakoutThreshold:         params.Float("breakoutThreshold"),
		StopMode:                  stopMode,
		ATRPeriod:                 params.Int("atrPeriod"),
		ATRMultiplier:             params.Float("atrMultiplier"),
		RewardRatio:               rewardRatio,
		RangeUseCap:               paramsBoolDefault(params, "rangeUseCap", true),
		VolumeFilter:              paramsBoolDefault(params, "volumeFilter", true),
		VolumeMinRatio:            volMin,
	}.normalized()
	return &PrevDayLevelBreakout{
		opts:    opts,
		buffer:  newCandleBuffer(120),
		session: ctx.Session,
	}, nil
}

func (o prevDayLevelOpts) normalized() prevDayLevelOpts {
	out := o
	if out.StrategyEntryDelayMinutes <= 0 {
		out.StrategyEntryDelayMinutes = 60
	}
	if out.EntryEndMinutes <= 0 {
		out.EntryEndMinutes = 360
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
	if out.VolumeMinRatio <= 0 {
		out.VolumeMinRatio = defaultVolumeMinRatio
	}
	return out
}

func prevDayLevelBreakoutConfigFields(params Params, ctx BuildContext) map[string]interface{} {
	rewardRatio := params.Float("rewardRatio")
	if rewardRatio <= 0 {
		rewardRatio = 2.0
	}
	volMin := params.Float("volumeMinRatio")
	if volMin <= 0 {
		volMin = params.Float("volumeFilterMultiplier")
	}
	entryDelay := params.Int("strategyEntryDelayMinutes")
	if entryDelay <= 0 {
		entryDelay = 60
	}
	tradeEnd := params.Int("entryEndMinutes")
	if tradeEnd <= 0 {
		tradeEnd = params.Int("fadeTradeEndMinutes")
	}
	if tradeEnd <= 0 {
		tradeEnd = 360
	}
	maxEntries := params.Int("maxEntriesPerTickerPerDay")
	if maxEntries <= 0 {
		maxEntries = 1
	}
	return map[string]interface{}{
		"stop_mode":                     ctx.StopMode,
		"strategy_entry_delay_minutes":  entryDelay,
		"entry_end_minutes":             tradeEnd,
		"fade_trade_end_minutes":        tradeEnd,
		"breakout_threshold":            params.Float("breakoutThreshold"),
		"atr_period":                    params.Int("atrPeriod"),
		"atr_multiplier":                params.Float("atrMultiplier"),
		"reward_ratio":                  rewardRatio,
		"volume_filter":                 paramsBoolDefault(params, "volumeFilter", true),
		"volume_min_ratio":              volMin,
		"max_trades_per_ticker_per_day": maxEntries,
		"trail_activation_r":            params.Float("trailActivationR"),
		"trail_stage_max":               params.Int("trailStageMax"),
		"trail_breakeven_r":             params.Float("trailBreakevenR"),
	}
}
