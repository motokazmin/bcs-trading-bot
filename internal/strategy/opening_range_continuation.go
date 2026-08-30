package strategy

import (
	"math"
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
	commonStopOpts
}

func (s *OpeningRangeContinuation) stopCfg() stopConfig {
	return s.opts.applyTo(stopConfig{
		StopMode: s.opts.StopMode, ATRPeriod: s.opts.ATRPeriod,
		ATRMultiplier: s.opts.ATRMultiplier, RangeUseCap: s.opts.RangeUseCap,
		RewardRatio: s.opts.RewardRatio,
	})
}

type orcPendingLimit struct {
	direction      string
	limitPrice     float64
	upper          float64
	lower          float64
	placedAt       time.Time
	expiresAt      time.Time
	breakoutCandle models.Candle
}

// invalidated — свеча закрылась обратно внутри диапазона, т.е. пробой не состоялся.
// Ретест-лимит в этом случае снимается: иначе он гарантированно исполнится на
// возврате цены и позиция откроется против уже развернувшегося движения.
func (p *orcPendingLimit) invalidated(candle models.Candle) bool {
	switch p.direction {
	case "BUY":
		return candle.Close < p.upper
	case "SELL":
		return candle.Close > p.lower
	}
	return true
}

// fillPrice — цена исполнения лимитной заявки на баре.
// Лимит на покупку исполняется, когда рынок доходит до уровня; если бар открылся
// уже ниже уровня, заявка исполняется по цене открытия (лучше лимита), а не по
// самому уровню. Раньше фил всегда записывался по limitPrice — из-за этого сделка
// открывалась по цене, которой на рынке в тот момент не было.
func (p *orcPendingLimit) fillPrice(candle models.Candle) (float64, bool) {
	switch p.direction {
	case "BUY":
		if candle.Low > p.limitPrice {
			return 0, false
		}
		return math.Min(candle.Open, p.limitPrice), true
	case "SELL":
		if candle.High < p.limitPrice {
			return 0, false
		}
		return math.Max(candle.Open, p.limitPrice), true
	}
	return 0, false
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

	// Пре-проверка выполнимости: если стоп по текущей истории не считается или
	// уже, чем min_stop_bps, заявку не ставим. Финальные SL/TP считаются при филе
	// от фактической цены исполнения.
	if sl, _ := calcStopTP(direction, entry, s.orbHigh, s.orbLow, s.buffer.history, s.stopCfg()); sl == 0 {
		return nil
	}

	s.pending = &orcPendingLimit{
		direction:      direction,
		limitPrice:     entry,
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
	if p.invalidated(candle) {
		s.pending = nil
		return nil
	}

	fill, filled := p.fillPrice(candle)
	if !filled {
		return nil
	}

	// SL/TP считаются от фактической цены фила, а не от уровня диапазона:
	// иначе R позиции не совпадает с реальным риском по факту исполнения.
	sl, tp := calcStopTP(p.direction, fill, p.upper, p.lower, s.buffer.history, s.stopCfg())
	if sl == 0 {
		s.pending = nil
		return nil
	}

	order := buildOrder(candle, p.direction, fill, sl, tp, p.upper, p.lower)
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
		commonStopOpts:    commonStopOptsFromParams(params),
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
	return commonStopOptsFromParams(params).configFields(map[string]interface{}{
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
	})
}
