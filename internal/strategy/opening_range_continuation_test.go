package strategy

import (
	"testing"
	"time"

	"bcs-trading-bot/internal/models"
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

// orcAfterBreakout — стратегия с готовым pending BUY-лимитом на orbHigh=101
// (ORB 10:00–10:25 = [99, 101], пробойная свеча закрылась на 104).
func orcAfterBreakout(t *testing.T, extra Params) (CandleStrategy, time.Time) {
	t.Helper()
	params := Params{
		"orbMinutes": 30, "breakoutThreshold": 0, "rewardRatio": 2.60, "atrMultiplier": 2,
	}
	for k, v := range extra {
		params[k] = v
	}
	s, err := NewFromParams(IDOpeningRangeContinuation, params, BuildContext{
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
			Open:   100, High: 101, Low: 99, Close: 100, Volume: 1000,
			Timestamp: base.Add(time.Duration(m*5) * time.Minute),
		})
	}
	if o := s.OnCandle(models.Candle{
		Ticker: "MGNT",
		Open:   102, High: 105, Low: 102, Close: 104, Volume: 5000,
		Timestamp: base.Add(35 * time.Minute),
	}); o != nil {
		t.Fatal("пробой должен ставить лимит, а не входить сразу")
	}
	return s, base
}

// Свеча, закрывшаяся обратно внутри диапазона, отменяет ретест-лимит: пробой не
// состоялся, и вход по уровню на развороте — это ловля ножа.
func TestORCCancelsPendingWhenBreakoutInvalidated(t *testing.T) {
	s, base := orcAfterBreakout(t, nil)

	failed := models.Candle{
		Ticker: "MGNT",
		Open:   104, High: 104, Low: 100.5, Close: 100, Volume: 3000,
		Timestamp: base.Add(40 * time.Minute),
	}
	if o := s.OnCandle(failed); o != nil {
		t.Fatalf("сломанный пробой не должен исполнять лимит, получено %+v", o)
	}

	// Заявка снята: возврат цены к уровню позже входа уже не даёт.
	back := models.Candle{
		Ticker: "MGNT",
		Open:   100, High: 101.5, Low: 100, Close: 101.2, Volume: 3000,
		Timestamp: base.Add(45 * time.Minute),
	}
	if o := s.OnCandle(back); o != nil {
		t.Fatalf("снятая заявка не должна оживать, получено %+v", o)
	}
}

// Если бар открылся уже за уровнем, лимит исполняется по цене открытия, а не по
// уровню: раньше сделка записывалась по цене, которой на рынке не было.
func TestORCFillsAtBarOpenWhenGappedThroughLevel(t *testing.T) {
	s, base := orcAfterBreakout(t, nil)

	gap := models.Candle{
		Ticker: "MGNT",
		Open:   100, High: 102, Low: 99.5, Close: 101.5, Volume: 3000,
		Timestamp: base.Add(40 * time.Minute),
	}
	o := s.OnCandle(gap)
	if o == nil {
		t.Fatal("ожидался фил лимита")
	}
	if o.Price != 100 {
		t.Fatalf("фил должен быть по open=100, получено %.4f", o.Price)
	}
	// SL/TP считаются от фактического фила, а не от уровня 101.
	r := o.Price - o.StopLoss
	if r <= 0 {
		t.Fatalf("SL должен быть ниже входа, получено SL=%.4f", o.StopLoss)
	}
	if got := (o.TakeProfit - o.Price) / r; got < 2.59 || got > 2.61 {
		t.Fatalf("R:R должен считаться от фила, получено %.3f", got)
	}
}

// min_stop_bps отсекает сигналы, у которых стоп внутри микроструктурного шума.
func TestORCMinStopBpsRejectsNarrowStop(t *testing.T) {
	// Дистанция стопа здесь ~50 б.п. от цены: порог 30 пропускает, 100 — режет.
	sPass, base := orcAfterBreakout(t, Params{"minStopBps": 30})
	retest := models.Candle{
		Ticker: "MGNT",
		Open:   104, High: 104, Low: 100.5, Close: 101.2, Volume: 3000,
		Timestamp: base.Add(40 * time.Minute),
	}
	if o := sPass.OnCandle(retest); o == nil {
		t.Fatal("min_stop_bps=30 не должен резать стоп ~50 б.п.")
	}

	sBlock, base2 := orcAfterBreakout(t, Params{"minStopBps": 100})
	retest.Timestamp = base2.Add(40 * time.Minute)
	if o := sBlock.OnCandle(retest); o != nil {
		t.Fatalf("min_stop_bps=100 должен отсечь стоп ~50 б.п., получено %+v", o)
	}
}

// take_profit_enabled=false — фиксированного тейка нет, выход только по трейлингу/EOD.
func TestORCTakeProfitDisabled(t *testing.T) {
	s, base := orcAfterBreakout(t, Params{"takeProfitEnabled": 0})
	retest := models.Candle{
		Ticker: "MGNT",
		Open:   104, High: 104, Low: 100.5, Close: 101.2, Volume: 3000,
		Timestamp: base.Add(40 * time.Minute),
	}
	o := s.OnCandle(retest)
	if o == nil {
		t.Fatal("ожидался фил лимита")
	}
	if o.TakeProfit != 0 {
		t.Fatalf("тейк должен быть отключён, получено %.4f", o.TakeProfit)
	}
	if o.StopLoss <= 0 {
		t.Fatalf("стоп обязателен даже без тейка, получено %.4f", o.StopLoss)
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
