package strategy

import (
	"sync"
	"time"

	"bcs-trading-bot/internal/models"
)

func init() {
	Register(Descriptor{
		ID:                   IDOpeningRangeContinuation,
		DefaultSearchSpace:   "configs/strategies/orc.yaml",
		NewFromParams:        newORCFromParams,
		ParamsToConfigFields: orcConfigFields,
	})
	Register(Descriptor{
		ID:                   IDSessionORC,
		DefaultSearchSpace:   "configs/strategies/session-orc-evening.yaml",
		NewFromParams:        newSessionORCFromParams,
		ParamsToConfigFields: orcConfigFields,
	})
}

// ORCWhitelist — рабочий whitelist ORC (matrix 2026-07-11).
var ORCWhitelist = map[string]struct{}{
	"MGNT": {},
	"ROSN": {},
	"TATN": {},
}

// ORCBlacklist — тикеры без edge на ORC.
var ORCBlacklist = map[string]struct{}{
	"SBER": {},
	"GAZP": {},
	"LKOH": {},
	"NVTK": {},
	"MOEX": {},
	"CHMF": {},
}

// OpeningRangeContinuation — пробой утреннего диапазона с входом на ретесте лимитным ордером.
type OpeningRangeContinuation struct {
	mu sync.Mutex

	opts            orcOpts
	buffer          *candleBuffer
	session         SessionTimes
	allowAllTickers bool

	day         string
	orbHigh     float64
	orbLow      float64
	orbComplete bool
	inORB       bool

	pending *orcPendingLimit
}

type orcOpts struct {
	ORBMinutes        int
	BreakoutThreshold float64
	StopMode          string
	ATRPeriod         int
	ATRMultiplier     float64
	RewardRatio       float64
	RangeUseCap       bool
}

type orcPendingLimit struct {
	direction     string
	limitPrice    float64
	stopLoss      float64
	takeProfit    float64
	upper         float64
	lower         float64
	placedAt      time.Time
	expiresAt     time.Time
	breakoutCandle models.Candle
}

func (s *OpeningRangeContinuation) ID() string {
	if s.allowAllTickers {
		return IDSessionORC
	}
	return IDOpeningRangeContinuation
}

func (s *OpeningRangeContinuation) OnCandle(candle models.Candle) *models.Order {
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

	if s.pending != nil {
		if order := s.tryFillPending(candle); order != nil {
			return order
		}
		if s.pending != nil {
			s.buffer.push(candle)
			return nil
		}
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

	if s.pending != nil {
		return nil
	}

	close := candle.Close
	th := s.opts.BreakoutThreshold
	var direction string
	switch {
	case close > s.orbHigh:
		if th > 0 && close <= s.orbHigh*(1+th) {
			return nil
		}
		direction = "BUY"
	case close < s.orbLow:
		if th > 0 && close >= s.orbLow*(1-th) {
			return nil
		}
		direction = "SELL"
	default:
		return nil
	}

	entry := s.orbHigh
	if direction == "SELL" {
		entry = s.orbLow
	}

	stopCfg := stopConfig{
		StopMode: s.opts.StopMode, ATRPeriod: s.opts.ATRPeriod,
		ATRMultiplier: s.opts.ATRMultiplier, RangeUseCap: s.opts.RangeUseCap,
		RewardRatio: s.opts.RewardRatio,
	}
	sl, tp := calcStopTP(direction, entry, s.orbHigh, s.orbLow, s.buffer.history, stopCfg)
	if sl == 0 {
		return nil
	}

	s.pending = &orcPendingLimit{
		direction:      direction,
		limitPrice:     entry,
		stopLoss:       sl,
		takeProfit:     tp,
		upper:          s.orbHigh,
		lower:          s.orbLow,
		placedAt:       candle.Timestamp,
		expiresAt:      hourEnd(candle.Timestamp),
		breakoutCandle: candle,
	}
	return nil
}

func (s *OpeningRangeContinuation) isTickerAllowed(ticker string) bool {
	if s.allowAllTickers {
		return true
	}
	return tickerAllowed(ticker, ORCWhitelist, ORCBlacklist)
}

func (s *OpeningRangeContinuation) resetDay(day string) {
	s.day = day
	s.orbHigh = 0
	s.orbLow = 0
	s.orbComplete = false
	s.inORB = false
	s.pending = nil
}

func (s *OpeningRangeContinuation) updateORB(candle models.Candle) {
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

func (s *OpeningRangeContinuation) tryFillPending(candle models.Candle) *models.Order {
	if s.pending == nil {
		return nil
	}
	if !candle.Timestamp.Before(s.pending.expiresAt) {
		s.pending = nil
		return nil
	}

	p := s.pending
	filled := false
	switch p.direction {
	case "BUY":
		filled = candle.Low <= p.limitPrice
	case "SELL":
		filled = candle.High >= p.limitPrice
	}
	if !filled {
		return nil
	}

	order := buildOrder(candle, p.direction, p.limitPrice, p.stopLoss, p.takeProfit, p.upper, p.lower)
	if order == nil {
		s.pending = nil
		return nil
	}
	s.buffer.markSignal(p.breakoutCandle)
	s.pending = nil
	return order
}

func hourEnd(t time.Time) time.Time {
	loc := t.Location()
	return time.Date(t.Year(), t.Month(), t.Day(), t.Hour()+1, 0, 0, 0, loc)
}

func newORCFromParams(params Params, ctx BuildContext) (CandleStrategy, error) {
	return newORCFromParamsExt(params, ctx, false)
}

func newSessionORCFromParams(params Params, ctx BuildContext) (CandleStrategy, error) {
	return newORCFromParamsExt(params, ctx, true)
}

func newORCFromParamsExt(params Params, ctx BuildContext, allowAll bool) (CandleStrategy, error) {
	stopMode := ctx.StopMode
	if stopMode == "" {
		stopMode = StopModeATR
	}
	orbMin := params.Int("orbMinutes")
	if orbMin <= 0 {
		orbMin = 30
	}
	rewardRatio := params.Float("rewardRatio")
	if rewardRatio <= 0 {
		rewardRatio = 2.60
	}
	// YAML allow_all_tickers перекрывает дефолт типа (orc=false, session_orc=true).
	if _, ok := params["allowAllTickers"]; ok {
		allowAll = params.Bool("allowAllTickers")
	}
	opts := orcOpts{
		ORBMinutes:        orbMin,
		BreakoutThreshold: params.Float("breakoutThreshold"),
		StopMode:          stopMode,
		ATRPeriod:         params.Int("atrPeriod"),
		ATRMultiplier:     params.Float("atrMultiplier"),
		RewardRatio:       rewardRatio,
		RangeUseCap:       paramsBoolDefault(params, "rangeUseCap", true),
	}.normalized()
	return &OpeningRangeContinuation{
		opts:            opts,
		buffer:          newCandleBuffer(50),
		session:         ctx.Session,
		allowAllTickers: allowAll,
	}, nil
}

func (o orcOpts) normalized() orcOpts {
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
		out.RewardRatio = 2.60
	}
	return out
}

func orcConfigFields(params Params, ctx BuildContext) map[string]interface{} {
	rewardRatio := params.Float("rewardRatio")
	if rewardRatio <= 0 {
		rewardRatio = 2.60
	}
	return map[string]interface{}{
		"stop_mode":                     ctx.StopMode,
		"orb_minutes":                   params.Int("orbMinutes"),
		"breakout_threshold":            params.Float("breakoutThreshold"),
		"atr_period":                    params.Int("atrPeriod"),
		"atr_multiplier":                params.Float("atrMultiplier"),
		"reward_ratio":                  rewardRatio,
		"max_trades_per_ticker_per_day": params.Int("maxEntriesPerTickerPerDay"),
		"trail_activation_r":            params.Float("trailActivationR"),
		"trail_stage_max":               params.Int("trailStageMax"),
		"trail_breakeven_r":             params.Float("trailBreakevenR"),
		"allow_all_tickers":             params.Bool("allowAllTickers"),
	}
}
