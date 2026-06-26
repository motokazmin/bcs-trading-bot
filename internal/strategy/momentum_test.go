package strategy

import (
	"testing"
	"time"

	"bcs-trading-bot/pkg/models"
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
