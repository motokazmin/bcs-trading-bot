package strategy

import (
	"sync"
	"time"

	"bcs-trading-bot/internal/models"
)

func init() {
	Register(Descriptor{
		ID:                   IDOpeningRangeFade,
		DefaultSearchSpace:   "configs/strategies/or-fade.yaml",
		NewFromParams:        newORFadeFromParams,
		ParamsToConfigFields: orFadeConfigFields,
	})
	Register(Descriptor{
		ID:                   IDSessionORFade,
		DefaultSearchSpace:   "configs/strategies/session-or-fade-evening.yaml",
		NewFromParams:        newSessionORFadeFromParams,
		ParamsToConfigFields: orFadeConfigFields,
	})
}

// ORFadeWhitelist — рабочий whitelist OR Fade (wave3-narrow-afks, 2026-07-12).
var ORFadeWhitelist = map[string]struct{}{
	"LKOH": {},
	"CHMF": {},
	"TATN": {},
	"GAZP": {},
	"MOEX": {},
	"AFKS": {},
}

// ORFadeBlacklist — тикеры без edge на OR Fade.
var ORFadeBlacklist = map[string]struct{}{
	"SBER": {},
	"NVTK": {},
	"ROSN": {},
	"MGNT": {},
}

// OpeningRangeFade — fade ложного пробоя утреннего диапазона (зеркало ORC).
type OpeningRangeFade struct {
	mu sync.Mutex

	opts            orFadeOpts
	buffer          *candleBuffer
	session         SessionTimes
	allowAllTickers bool

	day         string
	orbHigh     float64
	orbLow      float64
	orbComplete bool
	inORB       bool

	watch *orFadeWatch
}

type orFadeOpts struct {
	ORBMinutes          int
	BreakoutThreshold   float64
	FadeWindowMinutes   int
	FadeTradeEndMinutes int
	RequireInsideRange  bool
	StopMode            string
	ATRPeriod           int
	ATRMultiplier       float64
	RewardRatio         float64
	RangeUseCap         bool
	commonStopOpts
}

type orFadeWatch struct {
	direction      string // "up" or "down" — направление ложного пробоя
	startedAt      time.Time
	breakoutCandle models.Candle
}

func (s *OpeningRangeFade) ID() string {
	if s.allowAllTickers {
		return IDSessionORFade
	}
	return IDOpeningRangeFade
}

func (s *OpeningRangeFade) OnCandle(candle models.Candle) *models.Order {
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

	if !s.orbComplete && mins < s.opts.ORBMinutes {
		s.updateORB(candle)
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
	case close > s.orbHigh*(1+th):
		s.watch = &orFadeWatch{
			direction:      "up",
			startedAt:      candle.Timestamp,
			breakoutCandle: candle,
		}
	case close < s.orbLow*(1-th):
		s.watch = &orFadeWatch{
			direction:      "down",
			startedAt:      candle.Timestamp,
			breakoutCandle: candle,
		}
	}

	return nil
}

func (s *OpeningRangeFade) tryFadeEntry(candle models.Candle, close float64) *models.Order {
	w := s.watch
	if w == nil {
		return nil
	}

	var direction string
	var failed bool
	switch w.direction {
	case "up":
		if s.opts.RequireInsideRange {
			failed = close <= s.orbHigh && close >= s.orbLow
		} else {
			failed = close < s.orbHigh
		}
		if failed {
			direction = "SELL"
		}
	case "down":
		if s.opts.RequireInsideRange {
			failed = close >= s.orbLow && close <= s.orbHigh
		} else {
			failed = close > s.orbLow
		}
		if failed {
			direction = "BUY"
		}
	}

	if !failed {
		return nil
	}

	entry := close
	stopCfg := s.opts.applyTo(stopConfig{
		StopMode: s.opts.StopMode, ATRPeriod: s.opts.ATRPeriod,
		ATRMultiplier: s.opts.ATRMultiplier, RangeUseCap: s.opts.RangeUseCap,
		RewardRatio: s.opts.RewardRatio,
	})
	sl, tp := calcStopTP(direction, entry, s.orbHigh, s.orbLow, s.buffer.history, stopCfg)
	if sl == 0 {
		s.watch = nil
		return nil
	}

	order := buildOrder(candle, direction, entry, sl, tp, s.orbHigh, s.orbLow)
	if order == nil {
		s.watch = nil
		return nil
	}
	s.buffer.markSignal(w.breakoutCandle)
	s.watch = nil
	return order
}

func (s *OpeningRangeFade) watchExpired(now time.Time) bool {
	if s.watch == nil || s.opts.FadeWindowMinutes <= 0 {
		return false
	}
	deadline := s.watch.startedAt.Add(time.Duration(s.opts.FadeWindowMinutes) * time.Minute)
	return !now.Before(deadline)
}

func (s *OpeningRangeFade) isTickerAllowed(ticker string) bool {
	if s.allowAllTickers {
		return true
	}
	return tickerAllowed(ticker, ORFadeWhitelist, ORFadeBlacklist)
}

func (s *OpeningRangeFade) resetDay(day string) {
	s.day = day
	s.orbHigh = 0
	s.orbLow = 0
	s.orbComplete = false
	s.inORB = false
	s.watch = nil
}

func (s *OpeningRangeFade) updateORB(candle models.Candle) {
	s.inORB = true
	if s.orbHigh == 0 && s.orbLow == 0 {
		s.orbHigh = candle.High
		s.orbLow = candle.Low
		return
	}
	if candle.High > s.orbHigh {
		s.orbHigh = candle.High
	}
	if candle.Low < s.orbLow {
		s.orbLow = candle.Low
	}
}

func newORFadeFromParams(params Params, ctx BuildContext) (CandleStrategy, error) {
	return newORFadeFromParamsExt(params, ctx, false)
}

func newSessionORFadeFromParams(params Params, ctx BuildContext) (CandleStrategy, error) {
	return newORFadeFromParamsExt(params, ctx, true)
}

func newORFadeFromParamsExt(params Params, ctx BuildContext, allowAll bool) (CandleStrategy, error) {
	stopMode := ctx.StopMode
	if stopMode == "" {
		stopMode = StopModeATR
	}
	orbMin := params.Int("orbMinutes")
	if orbMin <= 0 {
		orbMin = 15
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
		tradeEnd = 90
	}
	opts := orFadeOpts{
		ORBMinutes:          orbMin,
		BreakoutThreshold:   params.Float("breakoutThreshold"),
		FadeWindowMinutes:   fadeWindow,
		FadeTradeEndMinutes: tradeEnd,
		RequireInsideRange:  paramsBoolDefault(params, "requireInsideRange", true),
		StopMode:            stopMode,
		ATRPeriod:           params.Int("atrPeriod"),
		ATRMultiplier:       params.Float("atrMultiplier"),
		RewardRatio:         rewardRatio,
		RangeUseCap:         paramsBoolDefault(params, "rangeUseCap", true),
		commonStopOpts:      commonStopOptsFromParams(params),
	}.normalized()
	return &OpeningRangeFade{
		opts:            opts,
		buffer:          newCandleBuffer(50),
		session:         ctx.Session,
		allowAllTickers: allowAll,
	}, nil
}

func (o orFadeOpts) normalized() orFadeOpts {
	out := o
	if out.ORBMinutes <= 0 {
		out.ORBMinutes = 15
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
		out.FadeTradeEndMinutes = 90
	}
	return out
}

func orFadeConfigFields(params Params, ctx BuildContext) map[string]interface{} {
	rewardRatio := params.Float("rewardRatio")
	if rewardRatio <= 0 {
		rewardRatio = 1.5
	}
	return commonStopOptsFromParams(params).configFields(map[string]interface{}{
		"stop_mode":                     ctx.StopMode,
		"orb_minutes":                   params.Int("orbMinutes"),
		"breakout_threshold":            params.Float("breakoutThreshold"),
		"fade_window_minutes":           params.Int("fadeWindowMinutes"),
		"fade_trade_end_minutes":        params.Int("fadeTradeEndMinutes"),
		"require_inside_range":          paramsBoolDefault(params, "requireInsideRange", true),
		"atr_period":                    params.Int("atrPeriod"),
		"atr_multiplier":                params.Float("atrMultiplier"),
		"reward_ratio":                  rewardRatio,
		"max_trades_per_ticker_per_day": params.Int("maxEntriesPerTickerPerDay"),
		"trail_activation_r":            params.Float("trailActivationR"),
		"trail_stage_max":               params.Int("trailStageMax"),
		"trail_breakeven_r":             params.Float("trailBreakevenR"),
	})
}
