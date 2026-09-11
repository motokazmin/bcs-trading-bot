package strategy

import (
	"sync"
	"time"

	"bcs-trading-bot/internal/models"
)

func init() {
	Register(Descriptor{
		ID:                   IDAfternoonRangeFade,
		DefaultSearchSpace:   "configs/strategies/afternoon-range-fade.yaml",
		NewFromParams:        newAfternoonRangeFadeFromParams,
		ParamsToConfigFields: afternoonRangeFadeConfigFields,
	})
}

// AfternoonRangeFade — fade ложного пробоя послеобеденного диапазона (зеркало OR Fade).
type AfternoonRangeFade struct {
	mu sync.Mutex

	opts    afternoonRangeFadeOpts
	buffer  *candleBuffer
	session SessionTimes

	day           string
	rangeHigh     float64
	rangeLow      float64
	rangeComplete bool
	inRange       bool

	watch *orFadeWatch
}

type afternoonRangeFadeOpts struct {
	RangeStartMinutes   int
	RangeEndMinutes     int
	BreakoutThreshold   float64
	FadeWindowMinutes   int
	FadeTradeEndMinutes int
	RequireInsideRange  bool
	StopMode            string
	ATRPeriod           int
	ATRMultiplier       float64
	RewardRatio         float64
	RangeUseCap         bool
}

func (s *AfternoonRangeFade) ID() string { return IDAfternoonRangeFade }

func (s *AfternoonRangeFade) OnCandle(candle models.Candle) *models.Order {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.isTickerAllowed(candle.Ticker) {
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

	rangeStart := s.opts.RangeStartMinutes
	rangeEnd := s.opts.RangeEndMinutes
	if rangeStart <= 0 {
		rangeStart = 150
	}
	if rangeEnd <= 0 {
		rangeEnd = 240
	}

	if !s.rangeComplete && mins >= rangeStart && mins < rangeEnd {
		s.updateRange(candle)
		s.buffer.push(candle)
		return nil
	}

	if !s.rangeComplete && s.inRange && mins >= rangeEnd {
		s.rangeComplete = true
		s.inRange = false
	}

	s.buffer.push(candle)

	if !s.rangeComplete || s.rangeHigh <= s.rangeLow {
		return nil
	}

	if s.opts.FadeTradeEndMinutes > 0 && mins > s.opts.FadeTradeEndMinutes {
		s.watch = nil
		return nil
	}

	if s.watch != nil {
		if s.watchExpired(candle.Timestamp) {
			s.watch = nil
		}
	}

	close := candle.Close
	th := s.opts.BreakoutThreshold

	if s.watch != nil {
		if order := s.tryFadeEntry(candle, close); order != nil {
			return order
		}
	}

	switch {
	case close > s.rangeHigh*(1+th):
		s.watch = &orFadeWatch{
			direction:      "up",
			startedAt:      candle.Timestamp,
			breakoutCandle: candle,
		}
	case close < s.rangeLow*(1-th):
		s.watch = &orFadeWatch{
			direction:      "down",
			startedAt:      candle.Timestamp,
			breakoutCandle: candle,
		}
	}

	return nil
}

func (s *AfternoonRangeFade) tryFadeEntry(candle models.Candle, close float64) *models.Order {
	w := s.watch
	if w == nil {
		return nil
	}

	var direction string
	var failed bool
	switch w.direction {
	case "up":
		if s.opts.RequireInsideRange {
			failed = close <= s.rangeHigh && close >= s.rangeLow
		} else {
			failed = close < s.rangeHigh
		}
		if failed {
			direction = "SELL"
		}
	case "down":
		if s.opts.RequireInsideRange {
			failed = close >= s.rangeLow && close <= s.rangeHigh
		} else {
			failed = close > s.rangeLow
		}
		if failed {
			direction = "BUY"
		}
	}

	if !failed {
		return nil
	}

	entry := close
	stopCfg := stopConfig{
		StopMode: s.opts.StopMode, ATRPeriod: s.opts.ATRPeriod,
		ATRMultiplier: s.opts.ATRMultiplier, RangeUseCap: s.opts.RangeUseCap,
		RewardRatio: s.opts.RewardRatio,
	}
	sl, tp := calcStopTP(direction, entry, s.rangeHigh, s.rangeLow, s.buffer.history, stopCfg)
	if sl == 0 {
		s.watch = nil
		return nil
	}

	order := buildOrder(candle, direction, entry, sl, tp, s.rangeHigh, s.rangeLow)
	if order == nil {
		s.watch = nil
		return nil
	}
	s.buffer.markSignal(w.breakoutCandle)
	s.watch = nil
	return order
}

func (s *AfternoonRangeFade) watchExpired(now time.Time) bool {
	if s.watch == nil || s.opts.FadeWindowMinutes <= 0 {
		return false
	}
	deadline := s.watch.startedAt.Add(time.Duration(s.opts.FadeWindowMinutes) * time.Minute)
	return !now.Before(deadline)
}

func (s *AfternoonRangeFade) isTickerAllowed(ticker string) bool {
	return tickerAllowed(ticker, ORFadeWhitelist, ORFadeBlacklist)
}

func (s *AfternoonRangeFade) resetDay(day string) {
	s.day = day
	s.rangeHigh = 0
	s.rangeLow = 0
	s.rangeComplete = false
	s.inRange = false
	s.watch = nil
}

func (s *AfternoonRangeFade) updateRange(candle models.Candle) {
	s.inRange = true
	if s.rangeHigh == 0 && s.rangeLow == 0 {
		s.rangeHigh = candle.High
		s.rangeLow = candle.Low
		return
	}
	if candle.High > s.rangeHigh {
		s.rangeHigh = candle.High
	}
	if candle.Low < s.rangeLow {
		s.rangeLow = candle.Low
	}
}

func newAfternoonRangeFadeFromParams(params Params, ctx BuildContext) (CandleStrategy, error) {
	stopMode := ctx.StopMode
	if stopMode == "" {
		stopMode = StopModeATR
	}
	rangeStart := params.Int("rangeStartMinutes")
	if rangeStart <= 0 {
		rangeStart = 150
	}
	rangeEnd := params.Int("rangeEndMinutes")
	if rangeEnd <= 0 {
		rangeEnd = 240
	}
	rewardRatio := params.Float("rewardRatio")
	if rewardRatio <= 0 {
		rewardRatio = 1.5
	}
	fadeWindow := params.Int("fadeWindowMinutes")
	if fadeWindow <= 0 {
		fadeWindow = 30
	}
	tradeEnd := params.Int("fadeTradeEndMinutes")
	if tradeEnd <= 0 {
		tradeEnd = 390
	}
	opts := afternoonRangeFadeOpts{
		RangeStartMinutes:   rangeStart,
		RangeEndMinutes:     rangeEnd,
		BreakoutThreshold:   params.Float("breakoutThreshold"),
		FadeWindowMinutes:   fadeWindow,
		FadeTradeEndMinutes: tradeEnd,
		RequireInsideRange:  paramsBoolDefault(params, "requireInsideRange", true),
		StopMode:            stopMode,
		ATRPeriod:           params.Int("atrPeriod"),
		ATRMultiplier:       params.Float("atrMultiplier"),
		RewardRatio:         rewardRatio,
		RangeUseCap:         paramsBoolDefault(params, "rangeUseCap", true),
	}.normalized()
	return &AfternoonRangeFade{
		opts:    opts,
		buffer:  newCandleBuffer(50),
		session: ctx.Session,
	}, nil
}

func (o afternoonRangeFadeOpts) normalized() afternoonRangeFadeOpts {
	out := o
	if out.RangeStartMinutes <= 0 {
		out.RangeStartMinutes = 150
	}
	if out.RangeEndMinutes <= 0 {
		out.RangeEndMinutes = 240
	}
	if out.RangeEndMinutes <= out.RangeStartMinutes {
		out.RangeEndMinutes = out.RangeStartMinutes + 90
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
		out.RewardRatio = 1.5
	}
	if out.FadeWindowMinutes <= 0 {
		out.FadeWindowMinutes = 30
	}
	if out.FadeTradeEndMinutes <= 0 {
		out.FadeTradeEndMinutes = 390
	}
	return out
}

func afternoonRangeFadeConfigFields(params Params, ctx BuildContext) map[string]interface{} {
	rewardRatio := params.Float("rewardRatio")
	if rewardRatio <= 0 {
		rewardRatio = 1.5
	}
	rangeStart := params.Int("rangeStartMinutes")
	if rangeStart <= 0 {
		rangeStart = 150
	}
	rangeEnd := params.Int("rangeEndMinutes")
	if rangeEnd <= 0 {
		rangeEnd = 240
	}
	fadeWindow := params.Int("fadeWindowMinutes")
	if fadeWindow <= 0 {
		fadeWindow = 30
	}
	tradeEnd := params.Int("fadeTradeEndMinutes")
	if tradeEnd <= 0 {
		tradeEnd = 390
	}
	return map[string]interface{}{
		"stop_mode":                     ctx.StopMode,
		"range_start_minutes":           rangeStart,
		"range_end_minutes":             rangeEnd,
		"breakout_threshold":            params.Float("breakoutThreshold"),
		"fade_window_minutes":           fadeWindow,
		"fade_trade_end_minutes":        tradeEnd,
		"require_inside_range":          paramsBoolDefault(params, "requireInsideRange", true),
		"atr_period":                    params.Int("atrPeriod"),
		"atr_multiplier":                params.Float("atrMultiplier"),
		"reward_ratio":                  rewardRatio,
		"max_trades_per_ticker_per_day": params.Int("maxEntriesPerTickerPerDay"),
		"trail_activation_r":            params.Float("trailActivationR"),
		"trail_stage_max":               params.Int("trailStageMax"),
		"trail_breakeven_r":             params.Float("trailBreakevenR"),
	}
}
