package strategy

import (
	"math"
	"testing"
	"time"

	"bcs-trading-bot/internal/models"
)

func candle(ts time.Time, open, high, low, close float64) models.Candle {
	return models.Candle{
		Ticker:    "SBER",
		Open:      open,
		High:      high,
		Low:       low,
		Close:     close,
		Timestamp: ts,
	}
}

func candleVol(ts time.Time, open, high, low, close float64, volume int64) models.Candle {
	c := candle(ts, open, high, low, close)
	c.Volume = volume
	return c
}

func TestATRStopWiderThanRangeCap(t *testing.T) {
	base := time.Date(2026, 6, 25, 10, 0, 0, 0, time.UTC)

	rangeStrat := NewMomentumBreakout(Options{Lookback: 5, StopMode: StopModeRange, RangeUseCap: true})
	atrStrat := NewMomentumBreakout(Options{Lookback: 5, StopMode: StopModeATR, ATRPeriod: 3, ATRMultiplier: 2.0})

	// Накопление истории: боковик, затем пробой вверх.
	bars := []models.Candle{
		candle(base, 100, 101, 99, 100),
		candle(base.Add(5*time.Minute), 100, 101, 99, 100),
		candle(base.Add(10*time.Minute), 100, 101, 99, 100),
		candle(base.Add(15*time.Minute), 100, 101, 99, 100),
		candle(base.Add(20*time.Minute), 105, 106, 104, 105), // пробой
	}

	var rangeOrder, atrOrder *models.Order
	for i, bar := range bars {
		if o := rangeStrat.OnCandle(bar); o != nil {
			rangeOrder = o
			if i != len(bars)-1 {
				t.Fatalf("range signal too early at bar %d", i)
			}
		}
		if o := atrStrat.OnCandle(bar); o != nil {
			atrOrder = o
			if i != len(bars)-1 {
				t.Fatalf("atr signal too early at bar %d", i)
			}
		}
	}

	if rangeOrder == nil || atrOrder == nil {
		t.Fatal("expected signals on breakout bar")
	}

	rangeSLDist := rangeOrder.Price - rangeOrder.StopLoss
	atrSLDist := atrOrder.Price - atrOrder.StopLoss
	if atrSLDist <= rangeSLDist {
		t.Fatalf("ATR stop should be wider: range=%.4f atr=%.4f", rangeSLDist, atrSLDist)
	}
}

func TestRangeWithoutCapUsesHalfRange(t *testing.T) {
	base := time.Date(2026, 6, 25, 10, 0, 0, 0, time.UTC)

	withCap := NewMomentumBreakout(Options{Lookback: 5, StopMode: StopModeRange, RangeUseCap: true})
	noCap := NewMomentumBreakout(Options{Lookback: 5, StopMode: StopModeRange, RangeUseCap: false})

	bars := []models.Candle{
		candle(base, 100, 110, 90, 100),
		candle(base.Add(5*time.Minute), 100, 110, 90, 100),
		candle(base.Add(10*time.Minute), 100, 110, 90, 100),
		candle(base.Add(15*time.Minute), 100, 110, 90, 100),
		candle(base.Add(20*time.Minute), 200, 210, 190, 200),
	}

	var capped, wide *models.Order
	for _, bar := range bars {
		if o := withCap.OnCandle(bar); o != nil {
			capped = o
		}
		if o := noCap.OnCandle(bar); o != nil {
			wide = o
		}
	}

	if capped == nil || wide == nil {
		t.Fatal("expected signals")
	}

	cappedDist := capped.Price - capped.StopLoss
	wideDist := wide.Price - wide.StopLoss
	if wideDist <= cappedDist {
		t.Fatalf("no-cap stop should be wider: capped=%.4f wide=%.4f", cappedDist, wideDist)
	}
}

// TestMomentumBreakoutFromParamsDefaultsRangeUseCapTrue проверяет, что путь
// NewFromParams (используемый optimizer) по умолчанию включает капирование
// range-стопа так же, как это делает runtime-бот (StrategyConfig → NewFromParams).
// Регрессия: momentumBreakoutOptsFromParams инвертировал флаг rangeUseCap,
// из-за чего optimizer всегда считал range-стоп без капа (0.5*range вместо
// капа ~0.5% от цены) — стопы были в разы шире, чем в проде, что портило
// walk-forward результаты в stop_mode=range.
func TestMomentumBreakoutFromParamsDefaultsRangeUseCapTrue(t *testing.T) {
	base := time.Date(2026, 6, 25, 10, 0, 0, 0, time.UTC)
	bars := []models.Candle{
		candle(base, 100, 110, 90, 100),
		candle(base.Add(5*time.Minute), 100, 110, 90, 100),
		candle(base.Add(10*time.Minute), 100, 110, 90, 100),
		candle(base.Add(15*time.Minute), 100, 110, 90, 100),
		candle(base.Add(20*time.Minute), 200, 210, 190, 200),
	}

	fromParams, err := newMomentumBreakoutFromParams(Params{
		"lookback":     5,
		"volumeFilter": 0,
	}, BuildContext{StopMode: StopModeRange})
	if err != nil {
		t.Fatalf("newMomentumBreakoutFromParams: %v", err)
	}
	reference := NewMomentumBreakout(Options{Lookback: 5, StopMode: StopModeRange, RangeUseCap: true})

	var fromParamsOrder, referenceOrder *models.Order
	for _, bar := range bars {
		if o := fromParams.OnCandle(bar); o != nil {
			fromParamsOrder = o
		}
		if o := reference.OnCandle(bar); o != nil {
			referenceOrder = o
		}
	}

	if fromParamsOrder == nil || referenceOrder == nil {
		t.Fatal("expected signals on breakout bar")
	}

	fromParamsDist := fromParamsOrder.Price - fromParamsOrder.StopLoss
	referenceDist := referenceOrder.Price - referenceOrder.StopLoss
	if math.Abs(fromParamsDist-referenceDist) > 1e-9 {
		t.Fatalf("NewFromParams должен по умолчанию капировать range-стоп как runtime-бот: got=%.4f want=%.4f",
			fromParamsDist, referenceDist)
	}
}

func TestVolumeFilterRejectsLowVolumeBreakout(t *testing.T) {
	base := time.Date(2026, 6, 25, 10, 0, 0, 0, time.UTC)

	noFilter := NewMomentumBreakout(Options{Lookback: 5, StopMode: StopModeRange})
	withFilter := NewMomentumBreakout(Options{
		Lookback:       5,
		StopMode:       StopModeRange,
		VolumeFilter:   true,
		VolumeMinRatio: 1.5,
	})

	bars := []models.Candle{
		candleVol(base, 100, 101, 99, 100, 1000),
		candleVol(base.Add(5*time.Minute), 100, 101, 99, 100, 1000),
		candleVol(base.Add(10*time.Minute), 100, 101, 99, 100, 1000),
		candleVol(base.Add(15*time.Minute), 100, 101, 99, 100, 1000),
		candleVol(base.Add(20*time.Minute), 105, 106, 104, 105, 100), // пробой, объём ниже 1.5× среднего
	}

	var baseline, filtered *models.Order
	for _, bar := range bars {
		if o := noFilter.OnCandle(bar); o != nil {
			baseline = o
		}
		if o := withFilter.OnCandle(bar); o != nil {
			filtered = o
		}
	}
	if baseline == nil {
		t.Fatal("expected signal without volume filter")
	}
	if filtered != nil {
		t.Fatal("expected nil with low volume and volume filter enabled")
	}
}

func TestVolumeFilterAcceptsHighVolumeBreakout(t *testing.T) {
	base := time.Date(2026, 6, 25, 10, 0, 0, 0, time.UTC)

	strat := NewMomentumBreakout(Options{
		Lookback:       5,
		StopMode:       StopModeRange,
		VolumeFilter:   true,
		VolumeMinRatio: 1.5,
	})

	bars := []models.Candle{
		candleVol(base, 100, 101, 99, 100, 1000),
		candleVol(base.Add(5*time.Minute), 100, 101, 99, 100, 1000),
		candleVol(base.Add(10*time.Minute), 100, 101, 99, 100, 1000),
		candleVol(base.Add(15*time.Minute), 100, 101, 99, 100, 1000),
		candleVol(base.Add(20*time.Minute), 105, 106, 104, 105, 2000), // 2000 > 1.5×1000
	}

	var signal *models.Order
	for _, bar := range bars {
		if o := strat.OnCandle(bar); o != nil {
			signal = o
		}
	}
	if signal == nil {
		t.Fatal("expected signal with high volume breakout")
	}
}

func TestBreakoutLevelsInSignal(t *testing.T) {
	base := time.Date(2026, 6, 25, 10, 0, 0, 0, time.UTC)
	strat := NewMomentumBreakout(Options{Lookback: 5, StopMode: StopModeRange})

	bars := []models.Candle{
		candle(base, 100, 101, 99, 100),
		candle(base.Add(5*time.Minute), 100, 101, 99, 100),
		candle(base.Add(10*time.Minute), 100, 101, 99, 100),
		candle(base.Add(15*time.Minute), 100, 101, 99, 100),
		candle(base.Add(20*time.Minute), 105, 106, 104, 105),
	}

	var signal *models.Order
	for _, bar := range bars {
		if o := strat.OnCandle(bar); o != nil {
			signal = o
		}
	}
	if signal == nil {
		t.Fatal("expected signal")
	}
	if signal.BreakoutUpper != 101 || signal.BreakoutLower != 99 {
		t.Fatalf("breakout levels: upper=%.2f lower=%.2f, want 101/99", signal.BreakoutUpper, signal.BreakoutLower)
	}
}
