package strategy

import (
	"testing"
	"time"

	"bcs-trading-bot/pkg/models"
)

func feedRisingH1(s CandleStrategy, ticker string, hours int, base time.Time) {
	for h := 0; h < hours; h++ {
		hourBase := base.Add(time.Duration(h) * time.Hour)
		px := 100.0 + float64(h)
		for m := 0; m < 12; m++ {
			_ = s.OnCandle(models.Candle{
				Ticker: ticker, Timestamp: hourBase.Add(time.Duration(m*5) * time.Minute),
				Open: px, High: px + 0.3, Low: px - 0.3, Close: px, Volume: 1000,
			})
		}
	}
}

func feedFallingH1(s CandleStrategy, ticker string, hours int, base time.Time) {
	for h := 0; h < hours; h++ {
		hourBase := base.Add(time.Duration(h) * time.Hour)
		px := 200.0 - float64(h)
		for m := 0; m < 12; m++ {
			_ = s.OnCandle(models.Candle{
				Ticker: ticker, Timestamp: hourBase.Add(time.Duration(m*5) * time.Minute),
				Open: px, High: px + 0.3, Low: px - 0.3, Close: px, Volume: 1000,
			})
		}
	}
}

func TestMomentumFilteredTrendGateBlocksAgainst(t *testing.T) {
	ctx := BuildContext{
		StopMode: StopModeATR,
		Session:  SessionTimes{Timezone: "Europe/Moscow", SessionOpenTime: "10:00"},
	}
	blocked, err := NewFromParams(IDMomentumFiltered, Params{
		"lookback": 5, "breakoutThreshold": 0, "rewardRatio": 2, "atrMultiplier": 2,
		"volumeFilter": 0, "longOnly": 0, "trendSMAPeriod": 0, "strategyEntryDelayMinutes": 0,
		"trendGateEnabled": 1, "trendFastPeriod": 3, "trendSlowPeriod": 5, "trendGateMode": 0,
	}, ctx)
	if err != nil {
		t.Fatal(err)
	}
	pass, err := NewFromParams(IDMomentumFiltered, Params{
		"lookback": 5, "breakoutThreshold": 0, "rewardRatio": 2, "atrMultiplier": 2,
		"volumeFilter": 0, "longOnly": 0, "trendSMAPeriod": 0, "strategyEntryDelayMinutes": 0,
		"trendGateEnabled": 1, "trendFastPeriod": 3, "trendSlowPeriod": 5, "trendGateMode": 0,
	}, ctx)
	if err != nil {
		t.Fatal(err)
	}

	base := time.Date(2024, 6, 3, 7, 0, 0, 0, time.UTC)
	// DOWN trend → BUY breakout must block
	feedFallingH1(blocked, "MGNT", 8, base)
	signalAt := base.Add(8 * time.Hour)
	// build lookback range then breakout up
	for i := 0; i < 5; i++ {
		p := 190.0
		_ = blocked.OnCandle(models.Candle{
			Ticker: "MGNT", Timestamp: signalAt.Add(time.Duration(i*5) * time.Minute),
			Open: p, High: p + 0.5, Low: p - 0.5, Close: p, Volume: 1000,
		})
	}
	bo := models.Candle{
		Ticker: "MGNT", Timestamp: signalAt.Add(25 * time.Minute),
		Open: 191, High: 195, Low: 191, Close: 194, Volume: 5000,
	}
	if o := blocked.OnCandle(bo); o != nil {
		t.Fatalf("BUY against DOWN trend must be blocked, got %+v", o)
	}

	// UP trend → BUY breakout must pass
	feedRisingH1(pass, "MGNT", 8, base)
	for i := 0; i < 5; i++ {
		p := 107.0
		_ = pass.OnCandle(models.Candle{
			Ticker: "MGNT", Timestamp: signalAt.Add(time.Duration(i*5) * time.Minute),
			Open: p, High: p + 0.5, Low: p - 0.5, Close: p, Volume: 1000,
		})
	}
	bo2 := models.Candle{
		Ticker: "MGNT", Timestamp: signalAt.Add(25 * time.Minute),
		Open: 108, High: 112, Low: 108, Close: 111, Volume: 5000,
	}
	if o := pass.OnCandle(bo2); o == nil || o.Direction != "BUY" {
		t.Fatalf("BUY with UP trend must pass, got %+v", o)
	}
}

func TestORCTrendGateBlocksAgainst(t *testing.T) {
	ctx := BuildContext{
		StopMode: StopModeATR,
		Session:  SessionTimes{Timezone: "Europe/Moscow", SessionOpenTime: "10:00"},
	}
	s, err := NewFromParams(IDOpeningRangeContinuation, Params{
		"orbMinutes": 30, "breakoutThreshold": 0, "rewardRatio": 2.6, "atrMultiplier": 2,
		"trendGateEnabled": 1, "trendFastPeriod": 3, "trendSlowPeriod": 5, "trendGateMode": 0,
	}, ctx)
	if err != nil {
		t.Fatal(err)
	}

	loc, _ := time.LoadLocation("Europe/Moscow")
	// Warm DOWN H1 overnight in UTC, then trade session in MSK
	warmBase := time.Date(2024, 6, 3, 0, 0, 0, 0, time.UTC)
	feedFallingH1(s, "MGNT", 8, warmBase)

	day := time.Date(2024, 6, 3, 10, 0, 0, 0, loc)
	for m := 0; m < 6; m++ {
		_ = s.OnCandle(models.Candle{
			Ticker: "MGNT", Timestamp: day.Add(time.Duration(m*5) * time.Minute),
			Open: 100, High: 101, Low: 99, Close: 100, Volume: 1000,
		})
	}
	breakout := models.Candle{
		Ticker: "MGNT", Timestamp: day.Add(35 * time.Minute),
		Open: 102, High: 105, Low: 102, Close: 104, Volume: 5000,
	}
	if o := s.OnCandle(breakout); o != nil {
		t.Fatal("breakout should not return immediate order")
	}
	retest := models.Candle{
		Ticker: "MGNT", Timestamp: day.Add(40 * time.Minute),
		Open: 104, High: 104, Low: 100.5, Close: 101, Volume: 3000,
	}
	if o := s.OnCandle(retest); o != nil {
		t.Fatalf("ORC BUY against DOWN H1 must not fill pending, got %+v", o)
	}
}

func TestMomentumFilteredTrendGateDisabledUnchanged(t *testing.T) {
	s, err := NewFromParams(IDMomentumFiltered, Params{
		"lookback": 5, "breakoutThreshold": 0, "rewardRatio": 2, "atrMultiplier": 2,
		"volumeFilter": 0, "longOnly": 0, "trendSMAPeriod": 0,
	}, BuildContext{StopMode: StopModeATR})
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2024, 6, 3, 10, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		p := 100.0
		_ = s.OnCandle(models.Candle{
			Ticker: "MGNT", Timestamp: base.Add(time.Duration(i*5) * time.Minute),
			Open: p, High: p + 1, Low: p - 1, Close: p, Volume: 1000,
		})
	}
	o := s.OnCandle(models.Candle{
		Ticker: "MGNT", Timestamp: base.Add(25 * time.Minute),
		Open: 102, High: 105, Low: 102, Close: 104, Volume: 5000,
	})
	if o == nil || o.Direction != "BUY" {
		t.Fatalf("gate disabled: expected BUY, got %+v", o)
	}
}
