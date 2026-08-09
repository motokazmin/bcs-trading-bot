package strategy

import (
	"sync"
	"time"

	"bcs-trading-bot/pkg/models"
)

const (
	// TrendGateModeBlock — не входить против H1-тренда.
	TrendGateModeBlock = 0
	// TrendGateModeWiden — входить против тренда, но расширить стоп.
	TrendGateModeWiden = 1

	defaultTrendFastPeriod       = 8
	defaultTrendSlowPeriod       = 21
	defaultTrendAgainstMult      = 1.5
	defaultTrendProviderMaxH1    = 120
)

// TrendGateOpts — параметры H1 trend-gate (опционально, default выключен).
type TrendGateOpts struct {
	Enabled            bool
	FastPeriod         int
	SlowPeriod         int
	Mode               int     // 0=block, 1=widen
	AgainstMultiplier  float64 // множитель дистанции стопа в режиме widen
}

func (o TrendGateOpts) normalized() TrendGateOpts {
	out := o
	if !out.Enabled {
		return out
	}
	if out.FastPeriod < 1 {
		out.FastPeriod = defaultTrendFastPeriod
	}
	if out.SlowPeriod < 2 {
		out.SlowPeriod = defaultTrendSlowPeriod
	}
	if out.FastPeriod >= out.SlowPeriod {
		out.FastPeriod = defaultTrendFastPeriod
		out.SlowPeriod = defaultTrendSlowPeriod
	}
	if out.Mode != TrendGateModeWiden {
		out.Mode = TrendGateModeBlock
	}
	if out.AgainstMultiplier <= 1 {
		out.AgainstMultiplier = defaultTrendAgainstMult
	}
	return out
}

func trendGateOptsFromParams(params Params) TrendGateOpts {
	return TrendGateOpts{
		Enabled:           params.Bool("trendGateEnabled"),
		FastPeriod:        params.Int("trendFastPeriod"),
		SlowPeriod:        params.Int("trendSlowPeriod"),
		Mode:              params.Int("trendGateMode"),
		AgainstMultiplier: params.Float("trendGateAgainstMultiplier"),
	}.normalized()
}

func mergeTrendGateConfigFields(m map[string]interface{}, params Params) {
	if !params.Bool("trendGateEnabled") {
		return
	}
	opts := trendGateOptsFromParams(params)
	m["trend_gate_enabled"] = true
	m["trend_fast_period"] = opts.FastPeriod
	m["trend_slow_period"] = opts.SlowPeriod
	m["trend_gate_mode"] = opts.Mode
	m["trend_gate_against_multiplier"] = opts.AgainstMultiplier
}

// calcEMA — EMA по Close; seed = SMA первых period баров. 0 если данных мало.
func calcEMA(history []models.Candle, period int) float64 {
	if period < 1 || len(history) < period {
		return 0
	}
	sum := 0.0
	for i := 0; i < period; i++ {
		sum += history[i].Close
	}
	ema := sum / float64(period)
	k := 2.0 / float64(period+1)
	for i := period; i < len(history); i++ {
		ema = history[i].Close*k + ema*(1-k)
	}
	return ema
}

// trendDirection — "UP" / "DOWN" / "" (insufficient data или flat).
func trendDirection(history []models.Candle, fastPeriod, slowPeriod int) string {
	if fastPeriod < 1 || slowPeriod < 2 || fastPeriod >= slowPeriod {
		return ""
	}
	if len(history) < slowPeriod {
		return ""
	}
	fast := calcEMA(history, fastPeriod)
	slow := calcEMA(history, slowPeriod)
	if fast == 0 || slow == 0 {
		return ""
	}
	if fast > slow {
		return "UP"
	}
	if fast < slow {
		return "DOWN"
	}
	return ""
}

// trendGate — чистая функция: пропустить ли сигнал.
// trend=="" → false (warmup=skip).
// mode block: только с трендом; widen: против тренда тоже true (widen решает вызывающий).
func trendGate(direction, trend string, mode int) bool {
	if trend == "" {
		return false
	}
	aligned := (direction == "BUY" && trend == "UP") || (direction == "SELL" && trend == "DOWN")
	if aligned {
		return true
	}
	return mode == TrendGateModeWiden
}

// trendGateAgainst — true, если сигнал против тренда (и trend известен).
func trendGateAgainst(direction, trend string) bool {
	if trend == "" {
		return false
	}
	aligned := (direction == "BUY" && trend == "UP") || (direction == "SELL" && trend == "DOWN")
	return !aligned
}

// widenStopTP расширяет дистанцию стопа в AgainstMultiplier раз, сохраняя исходный R:R.
func widenStopTP(direction string, entry, stopLoss, takeProfit, mult float64) (float64, float64) {
	if mult <= 1 {
		mult = defaultTrendAgainstMult
	}
	switch direction {
	case "BUY":
		oldDist := entry - stopLoss
		if oldDist <= 0 {
			return stopLoss, takeProfit
		}
		rr := (takeProfit - entry) / oldDist
		newDist := oldDist * mult
		return entry - newDist, entry + newDist*rr
	case "SELL":
		oldDist := stopLoss - entry
		if oldDist <= 0 {
			return stopLoss, takeProfit
		}
		rr := (entry - takeProfit) / oldDist
		newDist := oldDist * mult
		return entry + newDist, entry - newDist*rr
	default:
		return stopLoss, takeProfit
	}
}

// TrendProvider — общий ресэмплер M5→H1 и источник тренда по тикеру.
// Тренд считается только по закрытым H1 (без lookahead внутри текущего часа).
type TrendProvider struct {
	mu     sync.Mutex
	maxH1  int
	states map[string]*h1Agg
}

type h1Agg struct {
	openHour time.Time
	lastM5   time.Time
	partial  models.Candle
	closed   []models.Candle
}

// NewTrendProvider создаёт провайдер. maxH1<=0 → default.
func NewTrendProvider(maxH1 int) *TrendProvider {
	if maxH1 < 32 {
		maxH1 = defaultTrendProviderMaxH1
	}
	return &TrendProvider{
		maxH1:  maxH1,
		states: make(map[string]*h1Agg),
	}
}

// Observe агрегирует M5 в H1. Идемпотентен по (ticker, timestamp).
func (p *TrendProvider) Observe(c models.Candle) {
	if p == nil || c.Ticker == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	st := p.states[c.Ticker]
	if st == nil {
		st = &h1Agg{}
		p.states[c.Ticker] = st
	}

	ts := c.Timestamp.UTC()
	hour := ts.Truncate(time.Hour)

	if st.partial.Timestamp.IsZero() {
		st.openHour = hour
		st.lastM5 = ts
		st.partial = models.Candle{
			Ticker: c.Ticker, Timestamp: hour,
			Open: c.Open, High: c.High, Low: c.Low, Close: c.Close, Volume: c.Volume,
		}
		return
	}

	if hour.Equal(st.openHour) {
		if ts.Before(st.lastM5) {
			return
		}
		if ts.Equal(st.lastM5) {
			if c.High > st.partial.High {
				st.partial.High = c.High
			}
			if c.Low < st.partial.Low {
				st.partial.Low = c.Low
			}
			st.partial.Close = c.Close
			return
		}
		if c.High > st.partial.High {
			st.partial.High = c.High
		}
		if c.Low < st.partial.Low {
			st.partial.Low = c.Low
		}
		st.partial.Close = c.Close
		st.partial.Volume += c.Volume
		st.lastM5 = ts
		return
	}

	st.closed = append(st.closed, st.partial)
	if len(st.closed) > p.maxH1 {
		st.closed = st.closed[len(st.closed)-p.maxH1:]
	}
	st.openHour = hour
	st.lastM5 = ts
	st.partial = models.Candle{
		Ticker: c.Ticker, Timestamp: hour,
		Open: c.Open, High: c.High, Low: c.Low, Close: c.Close, Volume: c.Volume,
	}
}

// Warm прогревает провайдер историей M5 (для live; бэктест копит через Observe).
func (p *TrendProvider) Warm(candles []models.Candle) {
	for _, c := range candles {
		p.Observe(c)
	}
}

// Direction возвращает "UP"/"DOWN"/"" по закрытым H1.
func (p *TrendProvider) Direction(ticker string, fastPeriod, slowPeriod int) string {
	if p == nil {
		return ""
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	st := p.states[ticker]
	if st == nil {
		return ""
	}
	return trendDirection(st.closed, fastPeriod, slowPeriod)
}

// ClosedCount — число закрытых H1 (для тестов).
func (p *TrendProvider) ClosedCount(ticker string) int {
	if p == nil {
		return 0
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	st := p.states[ticker]
	if st == nil {
		return 0
	}
	return len(st.closed)
}

// applyTrendGate — общая точка перед buildOrder/pending.
// allow=false → не входить; widen=true → расширить стоп через AgainstMultiplier.
func applyTrendGate(opts TrendGateOpts, provider *TrendProvider, ticker, direction string) (allow bool, widen bool) {
	if !opts.Enabled {
		return true, false
	}
	trend := ""
	if provider != nil {
		trend = provider.Direction(ticker, opts.FastPeriod, opts.SlowPeriod)
	}
	if !trendGate(direction, trend, opts.Mode) {
		return false, false
	}
	if trendGateAgainst(direction, trend) && opts.Mode == TrendGateModeWiden {
		return true, true
	}
	return true, false
}
