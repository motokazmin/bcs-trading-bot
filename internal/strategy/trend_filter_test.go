package strategy

import (
	"testing"
	"time"

	"bcs-trading-bot/pkg/models"
)

func TestCalcEMA(t *testing.T) {
	// 5 одинаковых closes → EMA = close
	hist := make([]models.Candle, 5)
	for i := range hist {
		hist[i].Close = 100
	}
	if got := calcEMA(hist, 5); got != 100 {
		t.Fatalf("flat EMA: got %v want 100", got)
	}
	if got := calcEMA(hist, 10); got != 0 {
		t.Fatalf("insufficient: got %v want 0", got)
	}

	// rising series: EMA(3) should be > SMA-seed and < last close
	rising := []models.Candle{
		{Close: 10}, {Close: 11}, {Close: 12}, {Close: 13}, {Close: 14},
	}
	ema := calcEMA(rising, 3)
	if ema <= 11 || ema >= 14 {
		t.Fatalf("rising EMA(3)=%v, expected between 11 and 14", ema)
	}
}

func TestTrendDirection(t *testing.T) {
	up := make([]models.Candle, 30)
	for i := range up {
		up[i].Close = 100 + float64(i)
	}
	if got := trendDirection(up, 5, 13); got != "UP" {
		t.Fatalf("uptrend: got %q", got)
	}

	down := make([]models.Candle, 30)
	for i := range down {
		down[i].Close = 200 - float64(i)
	}
	if got := trendDirection(down, 5, 13); got != "DOWN" {
		t.Fatalf("downtrend: got %q", got)
	}

	if got := trendDirection(up[:10], 5, 13); got != "" {
		t.Fatalf("insufficient: got %q", got)
	}
	if got := trendDirection(up, 13, 5); got != "" {
		t.Fatalf("fast>=slow: got %q", got)
	}
}

func TestTrendGateWarmupSkip(t *testing.T) {
	if trendGate("BUY", "", TrendGateModeBlock) {
		t.Fatal("empty trend must skip (warmup policy)")
	}
	if trendGate("BUY", "", TrendGateModeWiden) {
		t.Fatal("empty trend must skip even in widen")
	}
}

func TestTrendGateBlockAndWiden(t *testing.T) {
	if !trendGate("BUY", "UP", TrendGateModeBlock) {
		t.Fatal("aligned BUY+UP should pass")
	}
	if trendGate("BUY", "DOWN", TrendGateModeBlock) {
		t.Fatal("against BUY+DOWN should block")
	}
	if !trendGate("BUY", "DOWN", TrendGateModeWiden) {
		t.Fatal("against in widen should pass gate")
	}
	if !trendGateAgainst("BUY", "DOWN") {
		t.Fatal("expected against")
	}
	if trendGateAgainst("BUY", "UP") {
		t.Fatal("aligned is not against")
	}
}

func TestWidenStopTP(t *testing.T) {
	sl, tp := widenStopTP("BUY", 100, 98, 106, 2)
	// oldDist=2, rr=3 → newDist=4 → sl=96, tp=112
	if sl != 96 || tp != 112 {
		t.Fatalf("BUY widen: sl=%v tp=%v", sl, tp)
	}
	sl, tp = widenStopTP("SELL", 100, 102, 94, 2)
	if sl != 104 || tp != 88 {
		t.Fatalf("SELL widen: sl=%v tp=%v", sl, tp)
	}
}

func TestTrendProviderResampleAndDirection(t *testing.T) {
	p := NewTrendProvider(80)
	base := time.Date(2024, 6, 3, 10, 0, 0, 0, time.UTC)

	// 30 closed H1 bars of rising closes (12 M5 each), then mid-hour M5
	for h := 0; h < 30; h++ {
		hourBase := base.Add(time.Duration(h) * time.Hour)
		closePx := 100.0 + float64(h)
		for m := 0; m < 12; m++ {
			p.Observe(models.Candle{
				Ticker: "SBER", Timestamp: hourBase.Add(time.Duration(m*5) * time.Minute),
				Open: closePx, High: closePx + 0.5, Low: closePx - 0.5, Close: closePx, Volume: 100,
			})
		}
	}
	if got := p.ClosedCount("SBER"); got != 29 {
		// last hour still forming → 29 closed
		t.Fatalf("closed H1: got %d want 29", got)
	}
	if dir := p.Direction("SBER", 5, 13); dir != "UP" {
		t.Fatalf("direction: got %q want UP", dir)
	}

	// duplicate Observe should not panic / inflate
	last := models.Candle{
		Ticker: "SBER", Timestamp: base.Add(29*time.Hour + 55*time.Minute),
		Open: 129, High: 130, Low: 128, Close: 129, Volume: 50,
	}
	p.Observe(last)
	p.Observe(last)
	if got := p.ClosedCount("SBER"); got != 29 {
		t.Fatalf("after dup closed: %d", got)
	}
}

func TestApplyTrendGateDisabled(t *testing.T) {
	allow, widen := applyTrendGate(TrendGateOpts{}, nil, "SBER", "BUY")
	if !allow || widen {
		t.Fatalf("disabled: allow=%v widen=%v", allow, widen)
	}
}
