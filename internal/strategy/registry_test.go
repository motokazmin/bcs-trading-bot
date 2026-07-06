package strategy

import (
	"testing"
	"time"

	"bcs-trading-bot/pkg/models"
)

func TestRegistryListsAllStrategies(t *testing.T) {
	ids := ListIDs()
	if len(ids) != 4 {
		t.Fatalf("registered: got %d, want 4: %v", len(ids), ids)
	}
}

func TestMomentumFilteredLongOnlyBlocksSell(t *testing.T) {
	s, err := NewFromParams(IDMomentumFiltered, Params{
		"lookback": 5, "breakoutThreshold": 0, "longOnly": 1,
		"rewardRatio": 2, "volumeFilter": 0, "atrMultiplier": 2,
	}, BuildContext{StopMode: StopModeRange})
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2024, 6, 1, 10, 0, 0, 0, time.UTC)
	price := 100.0
	for i := 0; i < 8; i++ {
		_ = s.OnCandle(models.Candle{
			Open: price, High: price + 1, Low: price - 1, Close: price,
			Volume: 100, Timestamp: start.Add(time.Duration(i*5) * time.Minute),
		})
		price -= 2
	}
	last := models.Candle{
		Open: 80, High: 81, Low: 79, Close: 78, Volume: 100,
		Timestamp: start.Add(40 * time.Minute),
	}
	if o := s.OnCandle(last); o != nil {
		t.Fatalf("expected no SELL in long-only, got %+v", o)
	}
}

func TestOpeningRangeSignalAfterWindow(t *testing.T) {
	s, err := NewFromParams(IDOpeningRange, Params{
		"orbMinutes": 30, "breakoutThreshold": 0, "rewardRatio": 2, "atrMultiplier": 2,
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
			Open: 100, High: 101, Low: 99, Close: 100, Volume: 1000,
			Timestamp: base.Add(time.Duration(m*5) * time.Minute),
		})
	}
	breakout := models.Candle{
		Open: 102, High: 105, Low: 102, Close: 104, Volume: 5000,
		Timestamp: base.Add(35 * time.Minute),
	}
	if o := s.OnCandle(breakout); o == nil {
		t.Fatal("expected ORB breakout signal")
	}
}

func TestMeanReversionFadeSignal(t *testing.T) {
	s, err := NewFromParams(IDMeanReversion, Params{
		"lookback": 5, "fadeThreshold": 0.01, "rewardRatio": 1.5, "volumeFilter": 0,
	}, BuildContext{StopMode: StopModeRange})
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2024, 6, 1, 10, 0, 0, 0, time.UTC)
	for i := 0; i < 6; i++ {
		p := 100.0 + float64(i)
		_ = s.OnCandle(models.Candle{
			Open: p, High: p + 1, Low: p - 1, Close: p, Volume: 100,
			Timestamp: start.Add(time.Duration(i*5) * time.Minute),
		})
	}
	spike := models.Candle{
		Open: 108, High: 110, Low: 107, Close: 109, Volume: 200,
		Timestamp: start.Add(30 * time.Minute),
	}
	if o := s.OnCandle(spike); o == nil || o.Direction != "SELL" {
		t.Fatalf("expected fade SELL, got %+v", o)
	}
}
