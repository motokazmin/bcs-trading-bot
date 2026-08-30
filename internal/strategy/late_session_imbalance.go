package strategy

import (
	"sync"

	"bcs-trading-bot/internal/models"
)

func init() {
	Register(Descriptor{
		ID:                   IDLateSessionImbalance,
		DefaultSearchSpace:   "configs/strategies/late-session-imbalance.yaml",
		NewFromParams:        newLateSessionImbalanceFromParams,
		ParamsToConfigFields: lateSessionImbalanceConfigFields,
	})
}

// LateSessionWhitelist — MOEX / LKOH.
var LateSessionWhitelist = map[string]struct{}{
	"MOEX": {},
	"LKOH": {},
}

// LateSessionImbalance — объёмный дисбаланс в конце сессии по дневному тренду.
type LateSessionImbalance struct {
	mu sync.Mutex

	opts    lateSessionOpts
	buffer  *candleBuffer
	session SessionTimes

	day         string
	sessionOpen float64
	dayVolSum   int64
	dayBars     int
	entered     bool
}

type lateSessionOpts struct {
	EntryStartMinutes int
	EntryEndMinutes   int
	StopMode          string
	ATRPeriod         int
	ATRMultiplier     float64
	RewardRatio       float64
	RangeUseCap       bool
	VolumeMinRatio    float64
}

func (s *LateSessionImbalance) ID() string { return IDLateSessionImbalance }

func (s *LateSessionImbalance) OnCandle(candle models.Candle) *models.Order {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !tickerAllowed(candle.Ticker, LateSessionWhitelist, nil) {
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

	if s.sessionOpen <= 0 {
		s.sessionOpen = candle.Open
	}

	s.buffer.push(candle)
	s.dayVolSum += candle.Volume
	s.dayBars++

	start := s.opts.EntryStartMinutes
	end := s.opts.EntryEndMinutes
	if start <= 0 {
		start = 480
	}
	if end <= 0 {
		end = 515
	}
	if s.entered || mins < start || mins > end {
		return nil
	}

	if s.dayBars < 2 || s.dayVolSum <= 0 {
		return nil
	}
	avgVol := float64(s.dayVolSum-candle.Volume) / float64(s.dayBars-1)
	if avgVol <= 0 {
		return nil
	}
	ratio := s.opts.VolumeMinRatio
	if ratio <= 0 {
		ratio = 2
	}
	if float64(candle.Volume) <= avgVol*ratio {
		return nil
	}

	var direction string
	if candle.Close > s.sessionOpen {
		direction = "BUY"
	} else if candle.Close < s.sessionOpen {
		direction = "SELL"
	} else {
		return nil
	}

	entry := candle.Close
	upper, lower := candle.High, candle.Low
	if len(s.buffer.history) >= 2 {
		if u, l, okRange := rangeLevels(s.buffer.history); okRange {
			upper, lower = u, l
		}
	}
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
	s.entered = true
	s.buffer.markSignal(candle)
	return order
}

func (s *LateSessionImbalance) resetDay(day string) {
	s.day = day
	s.sessionOpen = 0
	s.dayVolSum = 0
	s.dayBars = 0
	s.entered = false
}

func newLateSessionImbalanceFromParams(params Params, ctx BuildContext) (CandleStrategy, error) {
	stopMode := ctx.StopMode
	if stopMode == "" {
		stopMode = StopModeATR
	}
	start := params.Int("entryStartMinutes")
	if start <= 0 {
		start = params.Int("strategyEntryDelayMinutes")
	}
	if start <= 0 {
		start = 480
	}
	end := params.Int("entryEndMinutes")
	if end <= 0 {
		end = params.Int("fadeTradeEndMinutes")
	}
	if end <= 0 {
		end = 515
	}
	rewardRatio := params.Float("rewardRatio")
	if rewardRatio <= 0 {
		rewardRatio = 1.75
	}
	volMin := params.Float("volumeMinRatio")
	if volMin <= 0 {
		volMin = params.Float("volumeFilterMultiplier")
	}
	if volMin <= 0 {
		volMin = 2
	}
	opts := lateSessionOpts{
		EntryStartMinutes: start,
		EntryEndMinutes:   end,
		StopMode:          stopMode,
		ATRPeriod:         params.Int("atrPeriod"),
		ATRMultiplier:     params.Float("atrMultiplier"),
		RewardRatio:       rewardRatio,
		RangeUseCap:       paramsBoolDefault(params, "rangeUseCap", true),
		VolumeMinRatio:    volMin,
	}.normalized()
	return &LateSessionImbalance{
		opts:    opts,
		buffer:  newCandleBuffer(120),
		session: ctx.Session,
	}, nil
}

func (o lateSessionOpts) normalized() lateSessionOpts {
	out := o
	if out.EntryStartMinutes <= 0 {
		out.EntryStartMinutes = 480
	}
	if out.EntryEndMinutes <= 0 {
		out.EntryEndMinutes = 515
	}
	if out.StopMode == "" {
		out.StopMode = StopModeATR
	}
	if out.ATRPeriod < 2 {
		out.ATRPeriod = defaultATRPeriod
	}
	if out.ATRMultiplier <= 0 {
		out.ATRMultiplier = 1.0 // tight stop default
	}
	if out.RewardRatio <= 0 {
		out.RewardRatio = 1.75
	}
	if out.VolumeMinRatio <= 0 {
		out.VolumeMinRatio = 2
	}
	return out
}

func lateSessionImbalanceConfigFields(params Params, ctx BuildContext) map[string]interface{} {
	rewardRatio := params.Float("rewardRatio")
	if rewardRatio <= 0 {
		rewardRatio = 1.75
	}
	volMin := params.Float("volumeMinRatio")
	if volMin <= 0 {
		volMin = params.Float("volumeFilterMultiplier")
	}
	if volMin <= 0 {
		volMin = 2
	}
	start := params.Int("entryStartMinutes")
	if start <= 0 {
		start = 480
	}
	end := params.Int("entryEndMinutes")
	if end <= 0 {
		end = 515
	}
	maxEntries := params.Int("maxEntriesPerTickerPerDay")
	if maxEntries <= 0 {
		maxEntries = 1
	}
	return map[string]interface{}{
		"stop_mode":                     ctx.StopMode,
		"strategy_entry_delay_minutes":  start,
		"entry_start_minutes":           start,
		"entry_end_minutes":             end,
		"fade_trade_end_minutes":        end,
		"atr_period":                    params.Int("atrPeriod"),
		"atr_multiplier":                params.Float("atrMultiplier"),
		"reward_ratio":                  rewardRatio,
		"volume_filter":                 true,
		"volume_min_ratio":              volMin,
		"max_trades_per_ticker_per_day": maxEntries,
		"trail_activation_r":            params.Float("trailActivationR"),
		"trail_stage_max":               params.Int("trailStageMax"),
		"trail_breakeven_r":             params.Float("trailBreakevenR"),
	}
}
