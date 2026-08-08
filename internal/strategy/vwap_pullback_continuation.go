package strategy

import (
	"sync"

	"bcs-trading-bot/pkg/models"
)

func init() {
	Register(Descriptor{
		ID:                   IDVWAPPullbackContinuation,
		DefaultSearchSpace:   "configs/strategies/vwap-pullback-continuation.yaml",
		NewFromParams:        newVWAPPullbackFromParams,
		ParamsToConfigFields: vwapPullbackConfigFields,
	})
}

// VWAPWhitelist — universe VWAP pullback continuation (MGNT/ROSN/TATN).
var VWAPWhitelist = map[string]struct{}{
	"MGNT": {},
	"ROSN": {},
	"TATN": {},
}

// VWAPPullbackContinuation — OR breakout direction + VWAP pullback continuation.
type VWAPPullbackContinuation struct {
	mu sync.Mutex

	opts    vwapPullbackOpts
	buffer  *candleBuffer
	session SessionTimes

	day         string
	orbHigh     float64
	orbLow      float64
	orbComplete bool
	inORB       bool

	breakoutDir string // "BUY" / "SELL" after OR breakout
	sessionOpen float64
	cumTPV      float64
	cumVol      float64
	minsAbove   int
	minsBelow   int
	readyLong   bool
	readyShort  bool
}

type vwapPullbackOpts struct {
	ORBMinutes                int
	BreakoutThreshold         float64
	MinMinutesAboveVWAP       int
	StrategyEntryDelayMinutes int
	StopMode                  string
	ATRPeriod                 int
	ATRMultiplier             float64
	RewardRatio               float64
	RangeUseCap               bool
	VolumeFilter              bool
	VolumeMinRatio            float64
}

func (s *VWAPPullbackContinuation) ID() string { return IDVWAPPullbackContinuation }

func (s *VWAPPullbackContinuation) OnCandle(candle models.Candle) *models.Order {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !tickerAllowed(candle.Ticker, VWAPWhitelist, nil) {
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

	s.updateVWAP(candle)
	s.buffer.push(candle)

	if !s.orbComplete && mins < s.opts.ORBMinutes {
		s.updateORB(candle)
		return nil
	}
	if !s.orbComplete && s.inORB {
		s.orbComplete = true
		s.inORB = false
	}
	if !s.orbComplete || s.orbHigh <= s.orbLow {
		return nil
	}

	vwap := s.currentVWAP()
	if vwap <= 0 {
		return nil
	}

	s.trackVWAPSide(candle, vwap)

	if s.breakoutDir == "" {
		s.detectORBreakout(candle)
		return nil
	}

	entryDelay := s.opts.StrategyEntryDelayMinutes
	if entryDelay <= 0 {
		entryDelay = 35
	}
	if mins < entryDelay {
		return nil
	}

	if !passesVolumeFilter(s.buffer.history, candle, s.opts.VolumeFilter, s.opts.VolumeMinRatio) {
		return nil
	}

	var direction string
	switch s.breakoutDir {
	case "BUY":
		if s.readyLong && candle.Low <= vwap && candle.Close > vwap {
			direction = "BUY"
		}
	case "SELL":
		if s.readyShort && candle.High >= vwap && candle.Close < vwap {
			direction = "SELL"
		}
	}
	if direction == "" {
		return nil
	}

	entry := candle.Close
	stopCfg := stopConfig{
		StopMode: s.opts.StopMode, ATRPeriod: s.opts.ATRPeriod,
		ATRMultiplier: s.opts.ATRMultiplier, RangeUseCap: s.opts.RangeUseCap,
		RewardRatio: s.opts.RewardRatio,
	}
	sl, tp := calcStopTP(direction, entry, s.orbHigh, s.orbLow, s.buffer.history, stopCfg)
	order := buildOrder(candle, direction, entry, sl, tp, s.orbHigh, s.orbLow)
	if order == nil {
		return nil
	}
	s.readyLong = false
	s.readyShort = false
	s.minsAbove = 0
	s.minsBelow = 0
	s.buffer.markSignal(candle)
	return order
}

func (s *VWAPPullbackContinuation) updateVWAP(c models.Candle) {
	typical := (c.High + c.Low + c.Close) / 3
	vol := float64(c.Volume)
	if vol < 0 {
		vol = 0
	}
	s.cumTPV += typical * vol
	s.cumVol += vol
}

func (s *VWAPPullbackContinuation) currentVWAP() float64 {
	if s.cumVol <= 0 {
		return 0
	}
	return s.cumTPV / s.cumVol
}

func (s *VWAPPullbackContinuation) trackVWAPSide(c models.Candle, vwap float64) {
	const barMins = 5
	if c.Close > vwap {
		s.minsAbove += barMins
		s.minsBelow = 0
		if s.minsAbove >= s.opts.MinMinutesAboveVWAP {
			s.readyLong = true
		}
	} else if c.Close < vwap {
		s.minsBelow += barMins
		s.minsAbove = 0
		if s.minsBelow >= s.opts.MinMinutesAboveVWAP {
			s.readyShort = true
		}
	} else {
		s.minsAbove = 0
		s.minsBelow = 0
	}
}

func (s *VWAPPullbackContinuation) detectORBreakout(candle models.Candle) {
	close := candle.Close
	th := s.opts.BreakoutThreshold
	switch {
	case close > s.orbHigh:
		if th > 0 && close <= s.orbHigh*(1+th) {
			return
		}
		s.breakoutDir = "BUY"
	case close < s.orbLow:
		if th > 0 && close >= s.orbLow*(1-th) {
			return
		}
		s.breakoutDir = "SELL"
	}
}

func (s *VWAPPullbackContinuation) updateORB(candle models.Candle) {
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

func (s *VWAPPullbackContinuation) resetDay(day string) {
	s.day = day
	s.orbHigh = 0
	s.orbLow = 0
	s.orbComplete = false
	s.inORB = false
	s.breakoutDir = ""
	s.sessionOpen = 0
	s.cumTPV = 0
	s.cumVol = 0
	s.minsAbove = 0
	s.minsBelow = 0
	s.readyLong = false
	s.readyShort = false
}

func newVWAPPullbackFromParams(params Params, ctx BuildContext) (CandleStrategy, error) {
	stopMode := ctx.StopMode
	if stopMode == "" {
		stopMode = StopModeATR
	}
	orbMin := params.Int("orbMinutes")
	if orbMin <= 0 {
		orbMin = 30
	}
	minAbove := params.Int("minMinutesAboveVWAP")
	if minAbove <= 0 {
		minAbove = 30
	}
	entryDelay := params.Int("strategyEntryDelayMinutes")
	if entryDelay <= 0 {
		entryDelay = 35
	}
	rewardRatio := params.Float("rewardRatio")
	if rewardRatio <= 0 {
		rewardRatio = 2.0
	}
	volMin := params.Float("volumeMinRatio")
	if volMin <= 0 {
		volMin = params.Float("volumeFilterMultiplier")
	}
	opts := vwapPullbackOpts{
		ORBMinutes:                orbMin,
		BreakoutThreshold:         params.Float("breakoutThreshold"),
		MinMinutesAboveVWAP:       minAbove,
		StrategyEntryDelayMinutes: entryDelay,
		StopMode:                  stopMode,
		ATRPeriod:                 params.Int("atrPeriod"),
		ATRMultiplier:             params.Float("atrMultiplier"),
		RewardRatio:               rewardRatio,
		RangeUseCap:               paramsBoolDefault(params, "rangeUseCap", true),
		VolumeFilter:              paramsBoolDefault(params, "volumeFilter", true),
		VolumeMinRatio:            volMin,
	}.normalized()
	return &VWAPPullbackContinuation{
		opts:    opts,
		buffer:  newCandleBuffer(120),
		session: ctx.Session,
	}, nil
}

func (o vwapPullbackOpts) normalized() vwapPullbackOpts {
	out := o
	if out.ORBMinutes <= 0 {
		out.ORBMinutes = 30
	}
	if out.MinMinutesAboveVWAP <= 0 {
		out.MinMinutesAboveVWAP = 30
	}
	if out.StrategyEntryDelayMinutes <= 0 {
		out.StrategyEntryDelayMinutes = 35
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

func vwapPullbackConfigFields(params Params, ctx BuildContext) map[string]interface{} {
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
		entryDelay = 35
	}
	minAbove := params.Int("minMinutesAboveVWAP")
	if minAbove <= 0 {
		minAbove = 30
	}
	return map[string]interface{}{
		"stop_mode":                     ctx.StopMode,
		"orb_minutes":                   params.Int("orbMinutes"),
		"breakout_threshold":            params.Float("breakoutThreshold"),
		"min_minutes_above_vwap":        minAbove,
		"strategy_entry_delay_minutes":  entryDelay,
		"atr_period":                    params.Int("atrPeriod"),
		"atr_multiplier":                params.Float("atrMultiplier"),
		"reward_ratio":                  rewardRatio,
		"volume_filter":                 paramsBoolDefault(params, "volumeFilter", true),
		"volume_min_ratio":              volMin,
		"max_trades_per_ticker_per_day": params.Int("maxEntriesPerTickerPerDay"),
		"trail_activation_r":            params.Float("trailActivationR"),
		"trail_stage_max":               params.Int("trailStageMax"),
		"trail_breakeven_r":             params.Float("trailBreakevenR"),
	}
}
