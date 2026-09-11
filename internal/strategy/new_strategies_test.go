package strategy

import (
	"testing"
	"time"

	"bcs-trading-bot/internal/models"
)

func TestVWAPPullbackNewFromParamsNoPanic(t *testing.T) {
	s, err := NewFromParams(IDVWAPPullbackContinuation, Params{
		"orbMinutes": 30, "breakoutThreshold": 0, "minMinutesAboveVWAP": 10,
		"strategyEntryDelayMinutes": 35, "rewardRatio": 2, "atrMultiplier": 1.5,
		"volumeFilter": 0, "maxEntriesPerTickerPerDay": 2,
	}, BuildContext{
		StopMode: StopModeATR,
		Session:  SessionTimes{Timezone: "Europe/Moscow", SessionOpenTime: "10:00"},
	})
	if err != nil {
		t.Fatal(err)
	}
	loc, _ := time.LoadLocation("Europe/Moscow")
	base := time.Date(2024, 6, 3, 10, 0, 0, 0, loc)
	for i := 0; i < 20; i++ {
		p := 100.0 + float64(i)*0.1
		_ = s.OnCandle(models.Candle{
			Ticker: "MGNT", Open: p, High: p + 1, Low: p - 1, Close: p, Volume: 1000,
			Timestamp: base.Add(time.Duration(i*5) * time.Minute),
		})
	}
}

func TestMomentumSberDaytrendNewFromParamsNoPanic(t *testing.T) {
	s, err := NewFromParams(IDMomentumSberDaytrend, Params{
		"lookback": 5, "breakoutThreshold": 0, "longOnly": 1,
		"strategyEntryDelayMinutes": 10, "rewardRatio": 2, "volumeFilter": 0,
		"atrMultiplier": 2,
	}, BuildContext{
		StopMode: StopModeATR,
		Session:  SessionTimes{Timezone: "Europe/Moscow", SessionOpenTime: "10:00"},
	})
	if err != nil {
		t.Fatal(err)
	}
	loc, _ := time.LoadLocation("Europe/Moscow")
	d1 := time.Date(2024, 6, 3, 10, 0, 0, 0, loc)
	for i := 0; i < 8; i++ {
		_ = s.OnCandle(models.Candle{
			Ticker: "SBER", Open: 100, High: 101, Low: 99, Close: 100, Volume: 100,
			Timestamp: d1.Add(time.Duration(i*5) * time.Minute),
		})
	}
	d2 := time.Date(2024, 6, 4, 10, 0, 0, 0, loc)
	for i := 0; i < 10; i++ {
		p := 101.0 + float64(i)
		_ = s.OnCandle(models.Candle{
			Ticker: "SBER", Open: p, High: p + 2, Low: p - 1, Close: p + 1, Volume: 200,
			Timestamp: d2.Add(time.Duration(i*5) * time.Minute),
		})
	}
}

func TestMiddayCompressionNewFromParamsNoPanic(t *testing.T) {
	s, err := NewFromParams(IDMiddayCompressionBreakout, Params{
		"lookback": 4, "atrBars": 3, "compressionPercentile": 50,
		"rewardRatio": 2.2, "atrMultiplier": 1.5, "volumeFilter": 0,
	}, BuildContext{
		StopMode: StopModeATR,
		Session:  SessionTimes{Timezone: "Europe/Moscow", SessionOpenTime: "10:00"},
	})
	if err != nil {
		t.Fatal(err)
	}
	loc, _ := time.LoadLocation("Europe/Moscow")
	base := time.Date(2024, 6, 3, 10, 0, 0, 0, loc)
	for i := 0; i < 50; i++ {
		p := 100.0
		_ = s.OnCandle(models.Candle{
			Ticker: "LKOH", Open: p, High: p + 0.5, Low: p - 0.5, Close: p, Volume: 1000,
			Timestamp: base.Add(time.Duration(i*5) * time.Minute),
		})
	}
}

func TestLateSessionImbalanceNewFromParamsNoPanic(t *testing.T) {
	s, err := NewFromParams(IDLateSessionImbalance, Params{
		"entryStartMinutes": 480, "entryEndMinutes": 515,
		"volumeMinRatio": 1.5, "rewardRatio": 1.5, "atrMultiplier": 0.8,
	}, BuildContext{
		StopMode: StopModeATR,
		Session:  SessionTimes{Timezone: "Europe/Moscow", SessionOpenTime: "10:00"},
	})
	if err != nil {
		t.Fatal(err)
	}
	loc, _ := time.LoadLocation("Europe/Moscow")
	base := time.Date(2024, 6, 3, 10, 0, 0, 0, loc)
	for i := 0; i < 100; i++ {
		vol := int64(100)
		if i >= 96 {
			vol = 500
		}
		p := 100.0 + 0.1
		_ = s.OnCandle(models.Candle{
			Ticker: "MOEX", Open: 100, High: p + 1, Low: 99, Close: p, Volume: vol,
			Timestamp: base.Add(time.Duration(i*5) * time.Minute),
		})
	}
}

func TestPrevDayLevelBreakoutNewFromParamsNoPanic(t *testing.T) {
	s, err := NewFromParams(IDPrevDayLevelBreakout, Params{
		"strategyEntryDelayMinutes": 60, "entryEndMinutes": 360,
		"breakoutThreshold": 0.005, "rewardRatio": 2, "atrMultiplier": 1.5,
		"volumeFilter": 0, "maxEntriesPerTickerPerDay": 1,
	}, BuildContext{
		StopMode: StopModeATR,
		Session:  SessionTimes{Timezone: "Europe/Moscow", SessionOpenTime: "10:00"},
	})
	if err != nil {
		t.Fatal(err)
	}
	loc, _ := time.LoadLocation("Europe/Moscow")
	d1 := time.Date(2024, 6, 3, 10, 0, 0, 0, loc)
	for i := 0; i < 20; i++ {
		p := 100.0
		_ = s.OnCandle(models.Candle{
			Ticker: "MGNT", Open: p, High: p + 2, Low: p - 1, Close: p, Volume: 1000,
			Timestamp: d1.Add(time.Duration(i*5) * time.Minute),
		})
	}
	d2 := time.Date(2024, 6, 4, 10, 0, 0, 0, loc)
	for i := 0; i < 20; i++ {
		p := 103.0 + float64(i)*0.1
		_ = s.OnCandle(models.Candle{
			Ticker: "MGNT", Open: p, High: p + 1, Low: p - 1, Close: p, Volume: 2000,
			Timestamp: d2.Add(time.Duration(i*5) * time.Minute),
		})
	}
}

func TestAfternoonRangeFadeNewFromParamsNoPanic(t *testing.T) {
	s, err := NewFromParams(IDAfternoonRangeFade, Params{
		"rangeStartMinutes": 150, "rangeEndMinutes": 240,
		"breakoutThreshold": 0.005, "fadeWindowMinutes": 30,
		"fadeTradeEndMinutes": 390, "rewardRatio": 1.5, "atrMultiplier": 1.2,
		"requireInsideRange": 1,
	}, BuildContext{
		StopMode: StopModeATR,
		Session:  SessionTimes{Timezone: "Europe/Moscow", SessionOpenTime: "10:00"},
	})
	if err != nil {
		t.Fatal(err)
	}
	loc, _ := time.LoadLocation("Europe/Moscow")
	base := time.Date(2024, 6, 3, 10, 0, 0, 0, loc)
	for i := 0; i < 80; i++ {
		p := 100.0
		_ = s.OnCandle(models.Candle{
			Ticker: "LKOH", Open: p, High: p + 1, Low: p - 1, Close: p, Volume: 1000,
			Timestamp: base.Add(time.Duration(i*5) * time.Minute),
		})
	}
}
