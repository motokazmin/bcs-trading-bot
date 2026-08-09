package strategy

import (
	"fmt"
	"hash/fnv"
	"sync"

	"bcs-trading-bot/pkg/models"
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
type RandomEntry struct {
	mu sync.Mutex

	opts    randomEntryOpts
	buffer  *candleBuffer
	session SessionTimes
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
}

func (s *RandomEntry) ID() string { return IDRandomEntry }

func (s *RandomEntry) OnCandle(candle models.Candle) *models.Order {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.buffer.isDuplicateUpdate(candle) {
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

	entry := candle.Close
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
	s.buffer.markSignal(candle)
	return order
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
