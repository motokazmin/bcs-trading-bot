package strategy

import (
	"fmt"
	"hash/fnv"
	"math"
	"sync"

	"bcs-trading-bot/internal/models"
)

func init() {
	Register(Descriptor{
		ID:                   IDRandomEntry,
		DefaultSearchSpace:   "configs/strategies/random-entry.yaml",
		NewFromParams:        newRandomEntryFromParams,
		ParamsToConfigFields: randomEntryConfigFields,
	})
}

// RandomEntry — null-модель входа: случайный Buy/Sell в том же слоте/риск-каркасе.
// Max trades и session open режет engine; здесь только вероятность входа и направление.
//
// Два режима входа. По умолчанию — по close случайного бара (как fade/MF). При
// limitOffsetAtr > 0 — лимитом позади цены на offset·ATR от close, заявка живёт
// limitExpiryBars баров и исполняется так же, как ретест-лимит ORC. Этот режим гоняет
// лимитный путь исполнения (фил внутри бара, same-bar выход): случайный вход не может
// иметь преимущества, и плюс до издержек означает заглядывание вперёд в модели
// (docs/analysis/0005).
type RandomEntry struct {
	mu sync.Mutex

	opts    randomEntryOpts
	buffer  *candleBuffer
	session SessionTimes
	pending *randomPendingLimit
}

type randomPendingLimit struct {
	direction  string
	limitPrice float64
	day        string
	barsLeft   int
}

type randomEntryOpts struct {
	Lookback                  int
	StopMode                  string
	ATRPeriod                 int
	ATRMultiplier             float64
	RewardRatio               float64
	RangeUseCap               bool
	LongOnly                  bool
	StrategyEntryDelayMinutes int
	EntryProbability          float64
	Seed                      int64
	LimitOffsetATR            float64
	LimitExpiryBars           int
}

func (s *RandomEntry) ID() string { return IDRandomEntry }

func (s *RandomEntry) OnCandle(candle models.Candle) *models.Order {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.buffer.isDuplicateUpdate(candle) {
		return nil
	}
	if s.pending != nil {
		order := s.tryFillPending(candle)
		s.buffer.push(candle)
		if order != nil || s.pending != nil {
			return order
		}
		return nil
	}

	s.buffer.push(candle)
	if len(s.buffer.history) < s.opts.Lookback {
		return nil
	}

	if s.opts.StrategyEntryDelayMinutes > 0 {
		if mins, ok := s.session.minutesSinceOpen(candle.Timestamp); !ok || mins < s.opts.StrategyEntryDelayMinutes {
			return nil
		}
	}

	if s.opts.EntryProbability <= 0 {
		return nil
	}

	u, dirBit := barUnitInterval(s.opts.Seed, candle.Ticker, candle.Timestamp.UnixNano())
	if u >= s.opts.EntryProbability {
		return nil
	}

	direction := "BUY"
	if !s.opts.LongOnly && dirBit {
		direction = "SELL"
	}

	upper, lower, ok := rangeLevels(s.buffer.history)
	if !ok {
		entry := candle.Close
		upper, lower = entry*1.01, entry*0.99
	}

	if s.opts.LimitOffsetATR > 0 {
		atr := calcATR(s.buffer.history, s.opts.ATRPeriod)
		if atr <= 0 {
			return nil
		}
		limit := candle.Close - s.opts.LimitOffsetATR*atr
		if direction == "SELL" {
			limit = candle.Close + s.opts.LimitOffsetATR*atr
		}
		s.pending = &randomPendingLimit{
			direction:  direction,
			limitPrice: limit,
			day:        s.session.tradingDate(candle.Timestamp),
			barsLeft:   s.opts.LimitExpiryBars,
		}
		s.buffer.markSignal(candle)
		return nil
	}

	entry := candle.Close
	sl, tp := calcStopTP(direction, entry, upper, lower, s.buffer.history, s.stopCfg())
	order := buildOrder(candle, direction, entry, sl, tp, upper, lower)
	if order == nil {
		return nil
	}
	s.buffer.markSignal(candle)
	return order
}

func (s *RandomEntry) stopCfg() stopConfig {
	return stopConfig{
		StopMode: s.opts.StopMode, ATRPeriod: s.opts.ATRPeriod,
		ATRMultiplier: s.opts.ATRMultiplier, RangeUseCap: s.opts.RangeUseCap,
		RewardRatio: s.opts.RewardRatio,
	}
}

// tryFillPending — та же механика, что у ретест-лимита ORC: исполнение при касании,
// по open, если бар открылся за уровнем; SL/TP от фактического фила. Отмены по close
// бара нет — это было бы заглядывание вперёд.
func (s *RandomEntry) tryFillPending(candle models.Candle) *models.Order {
	p := s.pending
	if s.session.tradingDate(candle.Timestamp) != p.day || p.barsLeft <= 0 {
		s.pending = nil
		return nil
	}
	p.barsLeft--

	var fill float64
	switch p.direction {
	case "BUY":
		if candle.Low > p.limitPrice {
			return nil
		}
		fill = math.Min(candle.Open, p.limitPrice)
	case "SELL":
		if candle.High < p.limitPrice {
			return nil
		}
		fill = math.Max(candle.Open, p.limitPrice)
	}
	s.pending = nil

	upper, lower, ok := rangeLevels(s.buffer.history)
	if !ok {
		upper, lower = fill*1.01, fill*0.99
	}
	sl, tp := calcStopTP(p.direction, fill, upper, lower, s.buffer.history, s.stopCfg())
	return buildOrder(candle, p.direction, fill, sl, tp, upper, lower)
}

func newRandomEntryFromParams(params Params, ctx BuildContext) (CandleStrategy, error) {
	stopMode := ctx.StopMode
	if stopMode == "" {
		stopMode = StopModeATR
	}
	lookback := params.Int("lookback")
	atrPeriod := params.Int("atrPeriod")
	if atrPeriod < 2 {
		atrPeriod = defaultATRPeriod
	}
	if lookback < atrPeriod+1 {
		lookback = atrPeriod + 1
	}
	p := params.Float("entryProbability")
	if p < 0 {
		p = 0
	}
	if p > 1 {
		p = 1
	}
	opts := randomEntryOpts{
		Lookback:                  lookback,
		StopMode:                  stopMode,
		ATRPeriod:                 atrPeriod,
		ATRMultiplier:             params.Float("atrMultiplier"),
		RewardRatio:               params.Float("rewardRatio"),
		RangeUseCap:               paramsBoolDefault(params, "rangeUseCap", true),
		LongOnly:                  params.Bool("longOnly"),
		StrategyEntryDelayMinutes: params.Int("strategyEntryDelayMinutes"),
		EntryProbability:          p,
		Seed:                      int64(params.Int("seed")),
		LimitOffsetATR:            params.Float("limitOffsetAtr"),
		LimitExpiryBars:           params.Int("limitExpiryBars"),
	}
	if opts.LimitExpiryBars <= 0 {
		opts.LimitExpiryBars = 6
	}
	if opts.ATRMultiplier <= 0 {
		opts.ATRMultiplier = defaultATRMultiplier
	}
	if opts.RewardRatio <= 0 {
		opts.RewardRatio = DefaultRewardRatio(IDRandomEntry)
	}
	return &RandomEntry{
		opts:    opts,
		buffer:  newCandleBuffer(opts.Lookback),
		session: ctx.Session,
	}, nil
}

func randomEntryConfigFields(params Params, ctx BuildContext) map[string]interface{} {
	return map[string]interface{}{
		"lookback":                      params.Int("lookback"),
		"stop_mode":                     ctx.StopMode,
		"atr_period":                    params.Int("atrPeriod"),
		"atr_multiplier":                params.Float("atrMultiplier"),
		"reward_ratio":                  params.Float("rewardRatio"),
		"long_only":                     params.Bool("longOnly"),
		"strategy_entry_delay_minutes":  params.Int("strategyEntryDelayMinutes"),
		"entry_probability":             params.Float("entryProbability"),
		"seed":                          params.Int("seed"),
		"limit_offset_atr":              params.Float("limitOffsetAtr"),
		"limit_expiry_bars":             params.Int("limitExpiryBars"),
		"max_trades_per_ticker_per_day": params.Int("maxEntriesPerTickerPerDay"),
	}
}

// barUnitInterval возвращает u∈[0,1) и бит направления из детерминированного хеша бара.
func barUnitInterval(seed int64, ticker string, tsNano int64) (u float64, sellBit bool) {
	h := fnv.New64a()
	_, _ = fmt.Fprintf(h, "%d|%s|%d", seed, ticker, tsNano)
	v := h.Sum64()
	u = float64(v%1_000_000) / 1_000_000.0
	sellBit = (v/1_000_000)%2 == 1
	return u, sellBit
}
