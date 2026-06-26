package strategy

import (
	"math"
	"sync"

	"bcs-trading-bot/pkg/models"
)

const (
	defaultLookback = 20
	riskRewardRatio = 3.0
)

// MomentumBreakout — стратегия пробоя локальных уровней поддержки/сопротивления.
type MomentumBreakout struct {
	mu sync.Mutex

	opts           Options
	history        []models.Candle
	lastSignalTime int64
}

func NewMomentumBreakout(opts Options) *MomentumBreakout {
	opts = opts.normalized()
	return &MomentumBreakout{
		opts:    opts,
		history: make([]models.Candle, 0, opts.Lookback),
	}
}

// OnCandle принимает новую свечу и возвращает торговый сигнал или nil.
func (s *MomentumBreakout) OnCandle(candle models.Candle) *models.Order {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.isDuplicate(candle) {
		return nil
	}

	s.pushHistory(candle)

	if len(s.history) < s.opts.Lookback {
		return nil
	}

	upper, lower := s.levels()
	close := candle.Close

	var direction string
	switch {
	case close > upper:
		direction = "BUY"
	case close < lower:
		direction = "SELL"
	default:
		return nil
	}

	entry := close
	stopLoss, takeProfit := s.calcLevels(direction, entry, upper, lower)
	if stopLoss == 0 {
		return nil
	}

	s.lastSignalTime = candle.Timestamp.UnixNano()

	return &models.Order{
		Ticker:     candle.Ticker,
		Direction:  direction,
		Price:      entry,
		StopLoss:   stopLoss,
		TakeProfit: takeProfit,
	}
}

func (s *MomentumBreakout) isDuplicate(candle models.Candle) bool {
	ts := candle.Timestamp.UnixNano()
	if ts == s.lastSignalTime {
		return true
	}
	if len(s.history) > 0 {
		last := s.history[len(s.history)-1]
		if last.Timestamp.Equal(candle.Timestamp) {
			s.history[len(s.history)-1] = candle
			return true
		}
	}
	return false
}

func (s *MomentumBreakout) pushHistory(candle models.Candle) {
	if len(s.history) > 0 {
		last := s.history[len(s.history)-1]
		if last.Timestamp.Equal(candle.Timestamp) {
			s.history[len(s.history)-1] = candle
			return
		}
	}

	s.history = append(s.history, candle)
	if len(s.history) > s.opts.Lookback {
		s.history = s.history[len(s.history)-s.opts.Lookback:]
	}
}

func (s *MomentumBreakout) levels() (upper, lower float64) {
	window := s.history[:len(s.history)-1]
	upper = window[0].High
	lower = window[0].Low
	for _, c := range window[1:] {
		if c.High > upper {
			upper = c.High
		}
		if c.Low < lower {
			lower = c.Low
		}
	}
	return upper, lower
}

func (s *MomentumBreakout) calcLevels(direction string, entry, upper, lower float64) (stopLoss, takeProfit float64) {
	stopDistance := s.stopDistance(entry, upper, lower)
	if stopDistance <= 0 {
		return 0, 0
	}

	switch direction {
	case "BUY":
		stopLoss = entry - stopDistance
		takeProfit = entry + stopDistance*s.opts.RewardRatio
	case "SELL":
		stopLoss = entry + stopDistance
		takeProfit = entry - stopDistance*s.opts.RewardRatio
	}

	return stopLoss, takeProfit
}

func (s *MomentumBreakout) stopDistance(entry, upper, lower float64) float64 {
	rangeSize := upper - lower
	if rangeSize <= 0 {
		return 0
	}

	switch s.opts.StopMode {
	case StopModeATR:
		if atr := s.calcATR(); atr > 0 {
			return atr * s.opts.ATRMultiplier
		}
		return s.rangeStopDistance(entry, rangeSize)
	default:
		return s.rangeStopDistance(entry, rangeSize)
	}
}

func (s *MomentumBreakout) rangeStopDistance(entry, rangeSize float64) float64 {
	stopDistance := rangeSize * 0.5
	if s.opts.RangeUseCap {
		cap := entry * defaultRangeCapPct / 100
		stopDistance = math.Min(stopDistance, cap)
	}
	if stopDistance <= 0 {
		stopDistance = rangeSize * 0.25
	}
	return stopDistance
}

func (s *MomentumBreakout) calcATR() float64 {
	period := s.opts.ATRPeriod
	if len(s.history) < period+1 {
		return 0
	}

	start := len(s.history) - period
	var sum float64
	for i := start; i < len(s.history); i++ {
		c := s.history[i]
		prevClose := s.history[i-1].Close
		tr := math.Max(c.High-c.Low, math.Max(math.Abs(c.High-prevClose), math.Abs(c.Low-prevClose)))
		sum += tr
	}
	return sum / float64(period)
}
