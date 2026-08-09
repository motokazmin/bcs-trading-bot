package strategy

import (
	"testing"
	"time"

	"bcs-trading-bot/pkg/models"
)

func TestORCBlacklistIgnoresTicker(t *testing.T) {
	s, err := NewFromParams(IDOpeningRangeContinuation, Params{
		"orbMinutes": 30, "breakoutThreshold": 0, "rewardRatio": 2.60, "atrMultiplier": 2,
	}, BuildContext{
		StopMode: StopModeATR,
		Session: SessionTimes{
			Timezone: "Europe/Moscow", SessionOpenTime: "10:00",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	loc, _ := time.LoadLocation("Europe/Moscow")
	base := time.Date(2024, 6, 3, 10, 0, 0, 0, loc)

	for m := 0; m < 6; m++ {
		_ = s.OnCandle(models.Candle{
			Ticker: "LKOH",
			Open: 100, High: 101, Low: 99, Close: 100, Volume: 1000,
			Timestamp: base.Add(time.Duration(m*5) * time.Minute),
		})
	}
	breakout := models.Candle{
		Ticker: "LKOH",
		Open: 102, High: 105, Low: 102, Close: 104, Volume: 5000,
		Timestamp: base.Add(35 * time.Minute),
	}
	if o := s.OnCandle(breakout); o != nil {
		t.Fatalf("blacklisted LKOH should not signal, got %+v", o)
	}
}

func TestORCAllowAllTickersOverridesWhitelist(t *testing.T) {
	s, err := NewFromParams(IDOpeningRangeContinuation, Params{
		"orbMinutes": 30, "breakoutThreshold": 0, "rewardRatio": 2.60, "atrMultiplier": 2,
		"allowAllTickers": 1,
	}, BuildContext{
		StopMode: StopModeATR,
		Session: SessionTimes{
			Timezone: "Europe/Moscow", SessionOpenTime: "10:00",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	loc, _ := time.LoadLocation("Europe/Moscow")
	base := time.Date(2024, 6, 3, 10, 0, 0, 0, loc)

	for m := 0; m < 6; m++ {
		_ = s.OnCandle(models.Candle{
			Ticker: "LKOH",
			Open: 100, High: 101, Low: 99, Close: 100, Volume: 1000,
			Timestamp: base.Add(time.Duration(m*5) * time.Minute),
		})
	}
	_ = s.OnCandle(models.Candle{
		Ticker: "LKOH",
		Open: 102, High: 105, Low: 102, Close: 104, Volume: 5000,
		Timestamp: base.Add(35 * time.Minute),
	})
	fill := models.Candle{
		Ticker: "LKOH",
		Open: 103, High: 104, Low: 100, Close: 101, Volume: 2000,
		Timestamp: base.Add(40 * time.Minute),
	}
	if o := s.OnCandle(fill); o == nil {
		t.Fatal("allow_all_tickers: expected LKOH signal despite ORCBlacklist")
	}
}

func TestORCRetestLimitFill(t *testing.T) {
	s, err := NewFromParams(IDOpeningRangeContinuation, Params{
		"orbMinutes": 30, "breakoutThreshold": 0, "rewardRatio": 2.60, "atrMultiplier": 2,
	}, BuildContext{
		StopMode: StopModeATR,
		Session: SessionTimes{
			Timezone: "Europe/Moscow", SessionOpenTime: "10:00",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	loc, _ := time.LoadLocation("Europe/Moscow")
	base := time.Date(2024, 6, 3, 10, 0, 0, 0, loc)

	for m := 0; m < 6; m++ {
		_ = s.OnCandle(models.Candle{
			Ticker: "MGNT",
			Open: 100, High: 101, Low: 99, Close: 100, Volume: 1000,
			Timestamp: base.Add(time.Duration(m*5) * time.Minute),
		})
	}

	breakout := models.Candle{
		Ticker: "MGNT",
		Open: 102, High: 105, Low: 102, Close: 104, Volume: 5000,
		Timestamp: base.Add(35 * time.Minute),
	}
	if o := s.OnCandle(breakout); o != nil {
		t.Fatal("breakout should place pending limit, not immediate entry")
	}

	retest := models.Candle{
		Ticker: "MGNT",
		Open: 104, High: 104, Low: 100.5, Close: 101, Volume: 3000,
		Timestamp: base.Add(40 * time.Minute),
	}
	o := s.OnCandle(retest)
	if o == nil {
		t.Fatal("expected limit fill on retest")
	}
	if o.Direction != "BUY" {
		t.Fatalf("expected BUY, got %s", o.Direction)
	}
	if o.Price != 101 {
		t.Fatalf("expected entry at OR high 101, got %.2f", o.Price)
	}
}

func TestORCPendingExpiresWithoutPanic(t *testing.T) {
	s, err := NewFromParams(IDOpeningRangeContinuation, Params{
		"orbMinutes": 30, "breakoutThreshold": 0, "rewardRatio": 2.60, "atrMultiplier": 2,
	}, BuildContext{
		StopMode: StopModeATR,
		Session: SessionTimes{
			Timezone: "Europe/Moscow", SessionOpenTime: "10:00",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	loc, _ := time.LoadLocation("Europe/Moscow")
	base := time.Date(2024, 6, 3, 10, 0, 0, 0, loc)

	for m := 0; m < 6; m++ {
		_ = s.OnCandle(models.Candle{
			Ticker: "MGNT",
			Open: 100, High: 101, Low: 99, Close: 100, Volume: 1000,
			Timestamp: base.Add(time.Duration(m*5) * time.Minute),
		})
	}

	breakout := models.Candle{
		Ticker: "MGNT",
		Open: 102, High: 105, Low: 103, Close: 104, Volume: 5000,
		Timestamp: base.Add(35 * time.Minute),
	}
	if o := s.OnCandle(breakout); o != nil {
		t.Fatal("expected pending limit after breakout")
	}

	// Свеча в следующем часе без ретеста — pending истекает, паники быть не должно.
	expired := models.Candle{
		Ticker: "MGNT",
		Open: 104, High: 105, Low: 103, Close: 104, Volume: 1000,
		Timestamp: base.Add(65 * time.Minute),
	}
	if o := s.OnCandle(expired); o != nil {
		t.Fatalf("expected no fill after expiry, got %+v", o)
	}
}
