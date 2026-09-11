package strategy

import (
	"testing"
	"time"

	"bcs-trading-bot/internal/models"
)

func orFadeSession() BuildContext {
	return BuildContext{
		StopMode: StopModeATR,
		Session: SessionTimes{
			Timezone: "Europe/Moscow", SessionOpenTime: "10:00",
		},
	}
}

func TestORFadeSellOnFailedBreakoutUp(t *testing.T) {
	s, err := NewFromParams(IDOpeningRangeFade, Params{
		"orbMinutes": 15, "breakoutThreshold": 0, "fadeWindowMinutes": 30,
		"fadeTradeEndMinutes": 120, "requireInsideRange": 1,
		"rewardRatio": 1.5, "atrMultiplier": 1.5,
	}, orFadeSession())
	if err != nil {
		t.Fatal(err)
	}
	loc, _ := time.LoadLocation("Europe/Moscow")
	base := time.Date(2024, 6, 3, 10, 0, 0, 0, loc)
	for m := 0; m < 3; m++ {
		_ = s.OnCandle(models.Candle{
			Ticker: "LKOH", Open: 100, High: 101, Low: 99, Close: 100, Volume: 1000,
			Timestamp: base.Add(time.Duration(m*5) * time.Minute),
		})
	}
	breakout := models.Candle{
		Ticker: "LKOH", Open: 102, High: 105, Low: 102, Close: 104, Volume: 5000,
		Timestamp: base.Add(20 * time.Minute),
	}
	if o := s.OnCandle(breakout); o != nil {
		t.Fatal("breakout should arm watch, not enter immediately")
	}
	fail := models.Candle{
		Ticker: "LKOH", Open: 103, High: 103, Low: 99.5, Close: 100.5, Volume: 3000,
		Timestamp: base.Add(25 * time.Minute),
	}
	o := s.OnCandle(fail)
	if o == nil {
		t.Fatal("expected fade SELL on failed breakout up")
	}
	if o.Direction != "SELL" {
		t.Fatalf("want SELL, got %s", o.Direction)
	}
}

func TestORFadeBuyOnFailedBreakoutDown(t *testing.T) {
	s, err := NewFromParams(IDOpeningRangeFade, Params{
		"orbMinutes": 15, "breakoutThreshold": 0, "fadeWindowMinutes": 30,
		"fadeTradeEndMinutes": 120, "requireInsideRange": 1,
		"rewardRatio": 1.5, "atrMultiplier": 1.5,
	}, orFadeSession())
	if err != nil {
		t.Fatal(err)
	}
	loc, _ := time.LoadLocation("Europe/Moscow")
	base := time.Date(2024, 6, 3, 10, 0, 0, 0, loc)
	for m := 0; m < 3; m++ {
		_ = s.OnCandle(models.Candle{
			Ticker: "GAZP", Open: 100, High: 101, Low: 99, Close: 100, Volume: 1000,
			Timestamp: base.Add(time.Duration(m*5) * time.Minute),
		})
	}
	breakout := models.Candle{
		Ticker: "GAZP", Open: 98, High: 99, Low: 95, Close: 96, Volume: 5000,
		Timestamp: base.Add(20 * time.Minute),
	}
	if o := s.OnCandle(breakout); o != nil {
		t.Fatal("breakout should arm watch only")
	}
	fail := models.Candle{
		Ticker: "GAZP", Open: 97, High: 100.5, Low: 96.5, Close: 99.5, Volume: 3000,
		Timestamp: base.Add(25 * time.Minute),
	}
	o := s.OnCandle(fail)
	if o == nil || o.Direction != "BUY" {
		t.Fatalf("expected fade BUY, got %+v", o)
	}
}

func TestORFadeWatchExpires(t *testing.T) {
	s, err := NewFromParams(IDOpeningRangeFade, Params{
		"orbMinutes": 15, "breakoutThreshold": 0, "fadeWindowMinutes": 10,
		"fadeTradeEndMinutes": 120, "requireInsideRange": 1,
		"rewardRatio": 1.5, "atrMultiplier": 1.5,
	}, orFadeSession())
	if err != nil {
		t.Fatal(err)
	}
	loc, _ := time.LoadLocation("Europe/Moscow")
	base := time.Date(2024, 6, 3, 10, 0, 0, 0, loc)
	for m := 0; m < 3; m++ {
		_ = s.OnCandle(models.Candle{
			Ticker: "TATN", Open: 100, High: 101, Low: 99, Close: 100, Volume: 1000,
			Timestamp: base.Add(time.Duration(m*5) * time.Minute),
		})
	}
	_ = s.OnCandle(models.Candle{
		Ticker: "TATN", Open: 102, High: 105, Low: 102, Close: 104, Volume: 5000,
		Timestamp: base.Add(20 * time.Minute),
	})
	late := models.Candle{
		Ticker: "TATN", Open: 103, High: 103, Low: 99.5, Close: 100.5, Volume: 3000,
		Timestamp: base.Add(35 * time.Minute),
	}
	if o := s.OnCandle(late); o != nil {
		t.Fatal("watch should expire before fade entry")
	}
}

func TestORFadeBlacklistIgnoresTicker(t *testing.T) {
	s, err := NewFromParams(IDOpeningRangeFade, Params{
		"orbMinutes": 15, "breakoutThreshold": 0, "fadeWindowMinutes": 30,
		"fadeTradeEndMinutes": 120, "requireInsideRange": 1,
		"rewardRatio": 1.5, "atrMultiplier": 1.5,
	}, orFadeSession())
	if err != nil {
		t.Fatal(err)
	}
	loc, _ := time.LoadLocation("Europe/Moscow")
	base := time.Date(2024, 6, 3, 10, 0, 0, 0, loc)
	for m := 0; m < 3; m++ {
		_ = s.OnCandle(models.Candle{
			Ticker: "NVTK", Open: 100, High: 101, Low: 99, Close: 100, Volume: 1000,
			Timestamp: base.Add(time.Duration(m*5) * time.Minute),
		})
	}
	breakout := models.Candle{
		Ticker: "NVTK", Open: 102, High: 105, Low: 102, Close: 104, Volume: 5000,
		Timestamp: base.Add(20 * time.Minute),
	}
	if o := s.OnCandle(breakout); o != nil {
		t.Fatal("blacklisted NVTK should not signal")
	}
}
