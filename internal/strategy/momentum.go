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
// Хранит скользящее окно из последних N закрытых свечей и генерирует сигнал,
// когда цена закрытия пробивает диапазон предыдущих баров.
type MomentumBreakout struct {
	mu sync.Mutex

	lookback       int
	history        []models.Candle
	lastSignalTime int64 // unix nano, чтобы не дублировать сигнал на одной свече
}

func NewMomentumBreakout(lookback int) *MomentumBreakout {
	if lookback < 2 {
		lookback = defaultLookback
	}
	return &MomentumBreakout{
		lookback: lookback,
		history:  make([]models.Candle, 0, lookback),
	}
}

// OnCandle принимает новую свечу и возвращает торговый сигнал или nil.
// Потокобезопасен: можно вызывать из разных горутин.
func (s *MomentumBreakout) OnCandle(candle models.Candle) *models.Order {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.isDuplicate(candle) {
		return nil
	}

	s.pushHistory(candle)

	if len(s.history) < s.lookback {
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

	/*
		// ОТЛАДКА: искусственные сигналы на каждой 3-й свече (BUY/SELL поочерёдно).
		// Раскомментируйте блок ниже и закомментируйте production-логику выше,
		// чтобы быстро проверить симулятор сделок: SL/TP, EOD, Circuit Breaker
		// без ожидания реального пробоя уровней (~100 мин накопления истории).
		//
		// var candleCount int — добавьте поле в struct MomentumBreakout.
		//
		// s.candleCount++
		// if s.candleCount%3 != 0 {
		// 	return nil
		// }
		// direction := "BUY"
		// if (s.candleCount/3)%2 == 0 {
		// 	direction = "SELL"
		// }
		// entry := candle.Close
		// stopDistance := entry * 0.01
		// if stopDistance <= 0 {
		// 	stopDistance = 1
		// }
		// var stopLoss, takeProfit float64
		// switch direction {
		// case "BUY":
		// 	stopLoss = entry - stopDistance
		// 	takeProfit = entry + stopDistance*riskRewardRatio
		// case "SELL":
		// 	stopLoss = entry + stopDistance
		// 	takeProfit = entry - stopDistance*riskRewardRatio
		// }
		// s.lastSignalTime = candle.Timestamp.UnixNano()
		// return &models.Order{
		// 	Ticker: candle.Ticker, Direction: direction, Price: entry,
		// 	StopLoss: stopLoss, TakeProfit: takeProfit,
		// }
	*/
}

func (s *MomentumBreakout) isDuplicate(candle models.Candle) bool {
	ts := candle.Timestamp.UnixNano()
	if ts == s.lastSignalTime {
		return true
	}
	if len(s.history) > 0 {
		last := s.history[len(s.history)-1]
		if last.Timestamp.Equal(candle.Timestamp) {
			// Обновление текущей формирующейся свечи — перезаписываем, сигнал не генерируем повторно.
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
	if len(s.history) > s.lookback {
		s.history = s.history[len(s.history)-s.lookback:]
	}
}

// levels возвращает верхний и нижний уровни по High/Low предыдущих lookback-1 свечей.
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
	rangeSize := upper - lower
	if rangeSize <= 0 {
		return 0, 0
	}

	// Стоп за противоположной границей диапазона, но не дальше половины диапазона.
	stopDistance := math.Min(rangeSize*0.5, entry*0.005) // не более 0.5% от цены
	if stopDistance <= 0 {
		stopDistance = rangeSize * 0.25
	}

	switch direction {
	case "BUY":
		stopLoss = entry - stopDistance
		takeProfit = entry + stopDistance*riskRewardRatio
	case "SELL":
		stopLoss = entry + stopDistance
		takeProfit = entry - stopDistance*riskRewardRatio
	}

	return stopLoss, takeProfit
}
