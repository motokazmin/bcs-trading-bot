package strategy

import (
	"sync"

	"bcs-trading-bot/internal/models"
)

func init() {
	Register(Descriptor{
		ID:                   IDSessionGapDrive,
		DefaultSearchSpace:   "configs/strategies/session-gap-drive-evening.yaml",
		NewFromParams:        newSessionGapDriveFromParams,
		ParamsToConfigFields: sessionGapDriveConfigFields,
	})
}

// SessionGapDrive — вход по направлению gap относительно close предыдущего дня
// в начале текущей сессии (утро / вечер / ДСВД; окно задаётся SessionTimes).
type SessionGapDrive struct {
	mu sync.Mutex

	opts    gapDriveOpts
	buffer  *candleBuffer
	session SessionTimes

	day          string
	prevDayClose float64
	lastClose    float64
	entered      bool
}

type gapDriveOpts struct {
	GapThreshold          float64
	EntryEndMinutes       int
	StopMode              string
	ATRPeriod             int
	ATRMultiplier         float64
	RewardRatio           float64
	RangeUseCap           bool
	LongOnly              bool
}

func (s *SessionGapDrive) ID() string { return IDSessionGapDrive }

func (s *SessionGapDrive) OnCandle(candle models.Candle) *models.Order {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.buffer.isDuplicateUpdate(candle) {
		return nil
	}

	day := s.session.tradingDate(candle.Timestamp)
	if day != s.day {
		if s.day != "" && s.lastClose > 0 {
			s.prevDayClose = s.lastClose
		}
		s.day = day
		s.entered = false
	}
	s.lastClose = candle.Close

	mins, ok := s.session.minutesSinceOpen(candle.Timestamp)
	if !ok || mins < 0 {
		s.buffer.push(candle)
		return nil
	}
	if s.entered || s.prevDayClose <= 0 {
		s.buffer.push(candle)
		return nil
	}
	if s.opts.EntryEndMinutes > 0 && mins > s.opts.EntryEndMinutes {
		s.buffer.push(candle)
		return nil
	}

	gap := (candle.Close - s.prevDayClose) / s.prevDayClose
	th := s.opts.GapThreshold
	var direction string
	switch {
	case gap >= th:
		direction = "BUY"
	case gap <= -th && !s.opts.LongOnly:
		direction = "SELL"
	default:
		s.buffer.push(candle)
		return nil
	}

	entry := candle.Close
	stopCfg := stopConfig{
		StopMode: s.opts.StopMode, ATRPeriod: s.opts.ATRPeriod,
		ATRMultiplier: s.opts.ATRMultiplier, RangeUseCap: s.opts.RangeUseCap,
		RewardRatio: s.opts.RewardRatio,
	}
	// Диапазон для stop: recent high/low вокруг prev close.
	upper := s.prevDayClose
	lower := s.prevDayClose
	if candle.High > upper {
		upper = candle.High
	}
	if candle.Low < lower {
		lower = candle.Low
	}
	sl, tp := calcStopTP(direction, entry, upper, lower, s.buffer.history, stopCfg)
	if sl == 0 {
		s.buffer.push(candle)
		return nil
	}

	order := buildOrder(candle, direction, entry, sl, tp, upper, lower)
	if order == nil {
		s.buffer.push(candle)
		return nil
	}
	s.entered = true
	s.buffer.markSignal(candle)
	s.buffer.push(candle)
	return order
}

func newSessionGapDriveFromParams(params Params, ctx BuildContext) (CandleStrategy, error) {
	stopMode := ctx.StopMode
	if stopMode == "" {
		stopMode = StopModeATR
	}
	rewardRatio := params.Float("rewardRatio")
	if rewardRatio <= 0 {
		rewardRatio = 1.75
	}
	entryEnd := params.Int("entryEndMinutes")
	if entryEnd <= 0 {
		entryEnd = 60
	}
	opts := gapDriveOpts{
		GapThreshold:    params.Float("gapThreshold"),
		EntryEndMinutes: entryEnd,
		StopMode:        stopMode,
		ATRPeriod:       params.Int("atrPeriod"),
		ATRMultiplier:   params.Float("atrMultiplier"),
		RewardRatio:     rewardRatio,
		RangeUseCap:     paramsBoolDefault(params, "rangeUseCap", true),
		LongOnly:        paramsBoolDefault(params, "longOnly", false),
	}
	if opts.GapThreshold <= 0 {
		opts.GapThreshold = 0.003
	}
	if opts.ATRPeriod < 2 {
		opts.ATRPeriod = defaultATRPeriod
	}
	if opts.ATRMultiplier <= 0 {
		opts.ATRMultiplier = defaultATRMultiplier
	}
	return &SessionGapDrive{
		opts:    opts,
		buffer:  newCandleBuffer(50),
		session: ctx.Session,
	}, nil
}

func sessionGapDriveConfigFields(params Params, ctx BuildContext) map[string]interface{} {
	rewardRatio := params.Float("rewardRatio")
	if rewardRatio <= 0 {
		rewardRatio = 1.75
	}
	fields := map[string]interface{}{
		"stop_mode":                     ctx.StopMode,
		"gap_threshold":                 params.Float("gapThreshold"),
		"entry_end_minutes":             params.Int("entryEndMinutes"),
		"atr_period":                    params.Int("atrPeriod"),
		"atr_multiplier":                params.Float("atrMultiplier"),
		"reward_ratio":                  rewardRatio,
		"max_trades_per_ticker_per_day": params.Int("maxEntriesPerTickerPerDay"),
		"trail_activation_r":            params.Float("trailActivationR"),
		"trail_stage_max":               params.Int("trailStageMax"),
		"trail_breakeven_r":             params.Float("trailBreakevenR"),
	}
	if paramsBoolDefault(params, "longOnly", false) {
		fields["long_only"] = true
	}
	return fields
}
