package strategy

import (
	"math"
	"strconv"
	"strings"
	"time"

	"bcs-trading-bot/pkg/models"
)

const (
	defaultLookback        = 20
	defaultRiskRewardRatio = 3.0
)

type candleBuffer struct {
	history        []models.Candle
	maxLen         int
	lastSignalTime int64
}

func newCandleBuffer(maxLen int) *candleBuffer {
	if maxLen < 2 {
		maxLen = defaultLookback
	}
	return &candleBuffer{
		history: make([]models.Candle, 0, maxLen),
		maxLen:  maxLen,
	}
}

func (b *candleBuffer) push(candle models.Candle) {
	if len(b.history) > 0 {
		last := b.history[len(b.history)-1]
		if last.Timestamp.Equal(candle.Timestamp) {
			b.history[len(b.history)-1] = candle
			return
		}
	}
	b.history = append(b.history, candle)
	if len(b.history) > b.maxLen {
		b.history = b.history[len(b.history)-b.maxLen:]
	}
}

func (b *candleBuffer) isDuplicateUpdate(candle models.Candle) bool {
	ts := candle.Timestamp.UnixNano()
	if ts == b.lastSignalTime {
		return true
	}
	if len(b.history) > 0 {
		last := b.history[len(b.history)-1]
		if last.Timestamp.Equal(candle.Timestamp) {
			b.history[len(b.history)-1] = candle
			return true
		}
	}
	return false
}

func (b *candleBuffer) markSignal(candle models.Candle) {
	b.lastSignalTime = candle.Timestamp.UnixNano()
}

func rangeLevels(history []models.Candle) (upper, lower float64, ok bool) {
	if len(history) < 2 {
		return 0, 0, false
	}
	window := history[:len(history)-1]
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
	return upper, lower, true
}

func calcSMA(history []models.Candle, period int) float64 {
	if period < 1 || len(history) < period {
		return 0
	}
	start := len(history) - period
	sum := 0.0
	for i := start; i < len(history); i++ {
		sum += history[i].Close
	}
	return sum / float64(period)
}

func passesVolumeFilter(history []models.Candle, candle models.Candle, enabled bool, minRatio float64) bool {
	if !enabled {
		return true
	}
	if len(history) < 2 {
		return false
	}
	window := history[:len(history)-1]
	var sum int64
	for _, c := range window {
		sum += c.Volume
	}
	avg := float64(sum) / float64(len(window))
	if avg <= 0 {
		return false
	}
	if minRatio <= 0 {
		minRatio = defaultVolumeMinRatio
	}
	return float64(candle.Volume) > avg*minRatio
}

func calcATR(history []models.Candle, period int) float64 {
	if period < 2 || len(history) < period+1 {
		return 0
	}
	start := len(history) - period
	var sum float64
	for i := start; i < len(history); i++ {
		c := history[i]
		prevClose := history[i-1].Close
		tr := math.Max(c.High-c.Low, math.Max(math.Abs(c.High-prevClose), math.Abs(c.Low-prevClose)))
		sum += tr
	}
	return sum / float64(period)
}

type stopConfig struct {
	StopMode      string
	ATRPeriod     int
	ATRMultiplier float64
	RangeUseCap   bool
	RewardRatio   float64
}

func stopDistance(history []models.Candle, entry, upper, lower float64, cfg stopConfig) float64 {
	rangeSize := upper - lower
	if rangeSize <= 0 && cfg.StopMode != StopModeATR {
		return 0
	}
	switch cfg.StopMode {
	case StopModeATR:
		if atr := calcATR(history, cfg.ATRPeriod); atr > 0 {
			mult := cfg.ATRMultiplier
			if mult <= 0 {
				mult = defaultATRMultiplier
			}
			return atr * mult
		}
		if rangeSize <= 0 {
			return 0
		}
		return rangeStopDistance(entry, rangeSize, cfg.RangeUseCap)
	default:
		if rangeSize <= 0 {
			return 0
		}
		return rangeStopDistance(entry, rangeSize, cfg.RangeUseCap)
	}
}

func rangeStopDistance(entry, rangeSize float64, useCap bool) float64 {
	stopDistance := rangeSize * 0.5
	if useCap {
		cap := entry * defaultRangeCapPct / 100
		stopDistance = math.Min(stopDistance, cap)
	}
	if stopDistance <= 0 {
		stopDistance = rangeSize * 0.25
	}
	return stopDistance
}

func calcStopTP(direction string, entry, upper, lower float64, history []models.Candle, cfg stopConfig) (stopLoss, takeProfit float64) {
	dist := stopDistance(history, entry, upper, lower, cfg)
	if dist <= 0 {
		return 0, 0
	}
	rr := cfg.RewardRatio
	if rr <= 0 {
		rr = defaultRiskRewardRatio
	}
	switch direction {
	case "BUY":
		stopLoss = entry - dist
		takeProfit = entry + dist*rr
	case "SELL":
		stopLoss = entry + dist
		takeProfit = entry - dist*rr
	}
	return stopLoss, takeProfit
}

func buildOrder(candle models.Candle, direction string, entry, stopLoss, takeProfit, upper, lower float64) *models.Order {
	if stopLoss == 0 {
		return nil
	}
	return &models.Order{
		Ticker:        candle.Ticker,
		Direction:     direction,
		Price:         entry,
		StopLoss:      stopLoss,
		TakeProfit:    takeProfit,
		BreakoutUpper: upper,
		BreakoutLower: lower,
	}
}

// SessionTimes — параметры торговой сессии для стратегий с time filter / ORB.
type SessionTimes struct {
	Timezone          string
	SessionOpenTime   string
	EntryDelayMinutes int
}

func (s SessionTimes) minutesSinceOpen(t time.Time) (int, bool) {
	loc, err := time.LoadLocation(s.Timezone)
	if err != nil {
		return 0, false
	}
	local := t.In(loc)
	parts := splitHHMM(s.SessionOpenTime)
	if parts < 0 {
		return 0, false
	}
	openMin := parts
	curMin := local.Hour()*60 + local.Minute()
	return curMin - openMin - s.EntryDelayMinutes, true
}

func (s SessionTimes) tradingDate(t time.Time) string {
	loc, _ := time.LoadLocation(s.Timezone)
	if loc == nil {
		return t.Format("2006-01-02")
	}
	return t.In(loc).Format("2006-01-02")
}

func splitHHMM(value string) int {
	parts := strings.Split(strings.TrimSpace(value), ":")
	if len(parts) != 2 {
		return -1
	}
	h, err1 := strconv.Atoi(parts[0])
	m, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil || h < 0 || h > 23 || m < 0 || m > 59 {
		return -1
	}
	return h*60 + m
}
