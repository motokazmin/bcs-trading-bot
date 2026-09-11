package strategy

import (
	"testing"
	"time"

	"bcs-trading-bot/internal/models"
)

func TestRandomEntryLongOnlyBlocksSell(t *testing.T) {
	s, err := NewFromParams(IDRandomEntry, Params{
		"lookback": 5, "atrPeriod": 3, "atrMultiplier": 1.5,
		"rewardRatio": 1.5, "entryProbability": 1, "seed": 1, "longOnly": 1,
	}, BuildContext{StopMode: StopModeATR})
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2024, 6, 3, 10, 0, 0, 0, time.UTC)
	var got *models.Order
	for i := 0; i < 20; i++ {
		p := 100.0 + float64(i)*0.1
		o := s.OnCandle(models.Candle{
			Ticker: "SBER", Open: p, High: p + 1, Low: p - 1, Close: p,
			Volume: 1000, Timestamp: start.Add(time.Duration(i*5) * time.Minute),
		})
		if o != nil {
			got = o
			break
		}
	}
	if got == nil {
		t.Fatal("expected a signal with entryProbability=1")
	}
	if got.Direction != "BUY" {
		t.Fatalf("long-only: got %s, want BUY", got.Direction)
	}
}

func TestRandomEntrySeedStable(t *testing.T) {
	mk := func() CandleStrategy {
		s, err := NewFromParams(IDRandomEntry, Params{
			"lookback": 5, "atrPeriod": 3, "atrMultiplier": 1.5,
			"rewardRatio": 1.5, "entryProbability": 0.5, "seed": 42,
		}, BuildContext{StopMode: StopModeATR})
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	feed := func(s CandleStrategy) (dirs []string) {
		start := time.Date(2024, 6, 3, 10, 0, 0, 0, time.UTC)
		for i := 0; i < 40; i++ {
			p := 100.0
			o := s.OnCandle(models.Candle{
				Ticker: "GAZP", Open: p, High: p + 1, Low: p - 1, Close: p,
				Volume: 1000, Timestamp: start.Add(time.Duration(i*5) * time.Minute),
			})
			if o != nil {
				dirs = append(dirs, o.Direction)
			}
		}
		return dirs
	}
	a, b := feed(mk()), feed(mk())
	if len(a) == 0 {
		t.Fatal("expected some signals")
	}
	if len(a) != len(b) {
		t.Fatalf("signal count: %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("dir[%d]: %s vs %s", i, a[i], b[i])
		}
	}
}

func TestRandomEntryZeroProbabilityNoSignal(t *testing.T) {
	s, err := NewFromParams(IDRandomEntry, Params{
		"lookback": 5, "atrPeriod": 3, "atrMultiplier": 1.5,
		"rewardRatio": 1.5, "entryProbability": 0, "seed": 7,
	}, BuildContext{StopMode: StopModeATR})
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2024, 6, 3, 10, 0, 0, 0, time.UTC)
	for i := 0; i < 30; i++ {
		p := 100.0
		if o := s.OnCandle(models.Candle{
			Ticker: "LKOH", Open: p, High: p + 1, Low: p - 1, Close: p,
			Volume: 1000, Timestamp: start.Add(time.Duration(i*5) * time.Minute),
		}); o != nil {
			t.Fatalf("unexpected signal with p=0: %+v", o)
		}
	}
}

func TestBarUnitIntervalDeterministic(t *testing.T) {
	u1, b1 := barUnitInterval(99, "SBER", 1_700_000_000_000_000_000)
	u2, b2 := barUnitInterval(99, "SBER", 1_700_000_000_000_000_000)
	if u1 != u2 || b1 != b2 {
		t.Fatal("same inputs must match")
	}
	u3, _ := barUnitInterval(100, "SBER", 1_700_000_000_000_000_000)
	if u1 == u3 {
		t.Fatal("different seed should usually differ")
	}
}
