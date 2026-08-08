package strategy

import (
	"math"
	"sort"
	"sync"

	"bcs-trading-bot/pkg/models"
)

func init() {
	Register(Descriptor{
		ID:                   IDMiddayCompressionBreakout,
		DefaultSearchSpace:   "configs/strategies/midday-compression-breakout.yaml",
		NewFromParams:        newMiddayCompressionFromParams,
		ParamsToConfigFields: middayCompressionConfigFields,
	})
}

// MiddayWhitelist — LKOH / MOEX.
var MiddayWhitelist = map[string]struct{}{
	"LKOH": {},
	"MOEX": {},
}

// MiddayCompressionBreakout — сжатие ATR около полудня + пробой без ретеста.
type MiddayCompressionBreakout struct {
	mu sync.Mutex

	opts    middayCompressionOpts
	buffer  *candleBuffer
	session SessionTimes

	day           string
	dayATRs       []float64
	compressed    bool
	compressHigh  float64
	compressLow   float64
	enteredToday  bool
}

type middayCompressionOpts struct {
	Lookback                  int
	ATRBars                   int
	CompressionPercentile     float64
	CompressionStartMinutes   int
	CompressionEndMinutes     int
	EntryEndMinutes           int
	StopMode                  string
	ATRPeriod                 int
	ATRMultiplier             float64
	RewardRatio               float64
	RangeUseCap               bool
	VolumeFilter              bool
	VolumeMinRatio            float64
	BreakoutThreshold         float64
}

func (s *MiddayCompressionBreakout) ID() string { return IDMiddayCompressionBreakout }

func (s *MiddayCompressionBreakout) OnCandle(candle models.Candle) *models.Order {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !tickerAllowed(candle.Ticker, MiddayWhitelist, nil) {
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

	s.buffer.push(candle)

	atrBars := s.opts.ATRBars
	if atrBars < 2 {
		atrBars = 6
	}
	if atr := calcATR(s.buffer.history, atrBars); atr > 0 {
		s.dayATRs = append(s.dayATRs, atr)
	}

	cStart := s.opts.CompressionStartMinutes
	cEnd := s.opts.CompressionEndMinutes
	eEnd := s.opts.EntryEndMinutes
	if cStart <= 0 {
		cStart = 120
	}
	if cEnd <= 0 {
		cEnd = 150
	}
	if eEnd <= 0 {
		eEnd = 210
	}

	if mins >= cStart && mins <= cEnd && !s.compressed {
		s.tryMarkCompression()
	}

	if !s.compressed || s.enteredToday || mins < cEnd || mins > eEnd {
		return nil
	}

	lookback := s.opts.Lookback
	if lookback < 2 {
		lookback = 6
	}
	if len(s.buffer.history) < lookback+1 {
		return nil
	}

	upper, lower, okRange := rangeLevels(s.buffer.history[len(s.buffer.history)-lookback:])
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
		direction = "SELL"
	default:
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
	s.enteredToday = true
	s.buffer.markSignal(candle)
	return order
}

func (s *MiddayCompressionBreakout) tryMarkCompression() {
	if len(s.dayATRs) < 3 {
		return
	}
	pct := s.opts.CompressionPercentile
	if pct <= 0 || pct > 100 {
		pct = 40
	}
	latest := s.dayATRs[len(s.dayATRs)-1]
	threshold := percentile(s.dayATRs, pct)
	if latest > threshold {
		return
	}
	lookback := s.opts.Lookback
	if lookback < 2 {
		lookback = 6
	}
	hist := s.buffer.history
	if len(hist) < lookback {
		return
	}
	window := hist[len(hist)-lookback:]
	hi, lo := window[0].High, window[0].Low
	for _, c := range window[1:] {
		if c.High > hi {
			hi = c.High
		}
		if c.Low < lo {
			lo = c.Low
		}
	}
	s.compressed = true
	s.compressHigh = hi
	s.compressLow = lo
}

func percentile(values []float64, p float64) float64 {
	if len(values) == 0 {
		return 0
	}
	cp := append([]float64(nil), values...)
	sort.Float64s(cp)
	if p <= 0 {
		return cp[0]
	}
	if p >= 100 {
		return cp[len(cp)-1]
	}
	idx := (p / 100) * float64(len(cp)-1)
	lo := int(math.Floor(idx))
	hi := int(math.Ceil(idx))
	if lo == hi {
		return cp[lo]
	}
	w := idx - float64(lo)
	return cp[lo]*(1-w) + cp[hi]*w
}

func (s *MiddayCompressionBreakout) resetDay(day string) {
	s.day = day
	s.dayATRs = s.dayATRs[:0]
	s.compressed = false
	s.compressHigh = 0
	s.compressLow = 0
	s.enteredToday = false
}

func newMiddayCompressionFromParams(params Params, ctx BuildContext) (CandleStrategy, error) {
	stopMode := ctx.StopMode
	if stopMode == "" {
		stopMode = StopModeATR
	}
	atrBars := params.Int("atrBars")
	if atrBars <= 0 {
		atrBars = 6
	}
	lookback := params.Int("lookback")
	if lookback < 2 {
		lookback = 6
	}
	pct := params.Float("compressionPercentile")
	if pct <= 0 {
		pct = 40
	}
	rewardRatio := params.Float("rewardRatio")
	if rewardRatio <= 0 {
		rewardRatio = 2.25
	}
	volMin := params.Float("volumeMinRatio")
	if volMin <= 0 {
		volMin = params.Float("volumeFilterMultiplier")
	}
	cStart := params.Int("compressionStartMinutes")
	if cStart <= 0 {
		cStart = 120
	}
	cEnd := params.Int("compressionEndMinutes")
	if cEnd <= 0 {
		cEnd = 150
	}
	eEnd := params.Int("entryEndMinutes")
	if eEnd <= 0 {
		eEnd = params.Int("fadeTradeEndMinutes")
	}
	if eEnd <= 0 {
		eEnd = 210
	}
	atrPeriod := params.Int("atrPeriod")
	if atrPeriod < 2 {
		atrPeriod = defaultATRPeriod
	}
	opts := middayCompressionOpts{
		Lookback:                lookback,
		ATRBars:                 atrBars,
		CompressionPercentile:   pct,
		CompressionStartMinutes: cStart,
		CompressionEndMinutes:   cEnd,
		EntryEndMinutes:         eEnd,
		StopMode:                stopMode,
		ATRPeriod:               atrPeriod,
		ATRMultiplier:           params.Float("atrMultiplier"),
		RewardRatio:             rewardRatio,
		RangeUseCap:             paramsBoolDefault(params, "rangeUseCap", true),
		VolumeFilter:            paramsBoolDefault(params, "volumeFilter", true),
		VolumeMinRatio:          volMin,
		BreakoutThreshold:       params.Float("breakoutThreshold"),
	}.normalized()
	return &MiddayCompressionBreakout{
		opts:    opts,
		buffer:  newCandleBuffer(80),
		session: ctx.Session,
	}, nil
}

func (o middayCompressionOpts) normalized() middayCompressionOpts {
	out := o
	if out.Lookback < 2 {
		out.Lookback = 6
	}
	if out.ATRBars < 2 {
		out.ATRBars = 6
	}
	if out.CompressionPercentile <= 0 {
		out.CompressionPercentile = 40
	}
	if out.CompressionStartMinutes <= 0 {
		out.CompressionStartMinutes = 120
	}
	if out.CompressionEndMinutes <= 0 {
		out.CompressionEndMinutes = 150
	}
	if out.EntryEndMinutes <= 0 {
		out.EntryEndMinutes = 210
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
		out.RewardRatio = 2.25
	}
	if out.VolumeMinRatio <= 0 {
		out.VolumeMinRatio = defaultVolumeMinRatio
	}
	return out
}

func middayCompressionConfigFields(params Params, ctx BuildContext) map[string]interface{} {
	rewardRatio := params.Float("rewardRatio")
	if rewardRatio <= 0 {
		rewardRatio = 2.25
	}
	volMin := params.Float("volumeMinRatio")
	if volMin <= 0 {
		volMin = params.Float("volumeFilterMultiplier")
	}
	atrBars := params.Int("atrBars")
	if atrBars <= 0 {
		atrBars = 6
	}
	pct := params.Float("compressionPercentile")
	if pct <= 0 {
		pct = 40
	}
	lookback := params.Int("lookback")
	if lookback < 2 {
		lookback = 6
	}
	eEnd := params.Int("entryEndMinutes")
	if eEnd <= 0 {
		eEnd = 210
	}
	atrPeriod := params.Int("atrPeriod")
	if atrPeriod < 2 {
		atrPeriod = defaultATRPeriod
	}
	return map[string]interface{}{
		"stop_mode":                     ctx.StopMode,
		"lookback":                      lookback,
		"atr_period":                    atrPeriod,
		"atr_bars":                      atrBars,
		"compression_percentile":        pct,
		"fade_trade_end_minutes":        eEnd,
		"entry_end_minutes":             eEnd,
		"atr_multiplier":                params.Float("atrMultiplier"),
		"reward_ratio":                  rewardRatio,
		"breakout_threshold":            params.Float("breakoutThreshold"),
		"volume_filter":                 paramsBoolDefault(params, "volumeFilter", true),
		"volume_min_ratio":              volMin,
		"max_trades_per_ticker_per_day": params.Int("maxEntriesPerTickerPerDay"),
		"trail_activation_r":            params.Float("trailActivationR"),
		"trail_stage_max":               params.Int("trailStageMax"),
		"trail_breakeven_r":             params.Float("trailBreakevenR"),
	}
}
