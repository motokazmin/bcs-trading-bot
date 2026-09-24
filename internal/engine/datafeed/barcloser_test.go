package datafeed

import (
	"context"
	"testing"
	"time"

	"bcs-trading-bot/internal/engine/broker"
	"bcs-trading-bot/internal/models"
)

func routeKey(ticker, tf string) broker.RouteKey {
	return broker.RouteKey{Ticker: ticker, Timeframe: tf}
}

var t0 = time.Date(2026, 9, 18, 8, 0, 0, 0, time.UTC)

func bar(offset time.Duration, close float64) models.Candle {
	return models.Candle{Ticker: "CHMF", Timestamp: t0.Add(offset), Open: close, High: close, Low: close, Close: close}
}

// Регрессия 0004: стратегия получала первое обновление бара (заглушку первых
// секунд) вместо закрытого бара. Отдаваться обязан ПОСЛЕДНИЙ снимок бара.
func TestЗакрытыйБарЭтоПоследнееОбновление(t *testing.T) {
	b := newBarCloser(5 * time.Minute)

	for _, px := range []float64{627.6, 626.0, 625.6} {
		if _, ok := b.update(bar(0, px)); ok {
			t.Fatalf("бар отдан до закрытия на обновлении %.1f", px)
		}
	}
	closed, ok := b.update(bar(5*time.Minute, 625.8))
	if !ok {
		t.Fatal("новая метка не закрыла предыдущий бар")
	}
	if !closed.Timestamp.Equal(t0) || closed.Close != 625.6 {
		t.Fatalf("закрыт не тот снимок: %s close=%.1f, want %s close=625.6", closed.Timestamp, closed.Close, t0)
	}
}

func TestПоздниеИСтарыеОбновленияОтбрасываются(t *testing.T) {
	b := newBarCloser(5 * time.Minute)
	b.update(bar(0, 1))
	b.update(bar(5*time.Minute, 2)) // закрыл t0

	if _, ok := b.update(bar(0, 99)); ok {
		t.Fatal("позднее обновление уже отданного бара переиграло его")
	}
	closed, ok := b.update(bar(10*time.Minute, 3))
	if !ok || closed.Close != 2 {
		t.Fatalf("позднее обновление испортило текущий бар: ok=%v close=%.0f", ok, closed.Close)
	}
}

func TestБарЗакрываетсяПоТаймеруБезСледующего(t *testing.T) {
	b := newBarCloser(5 * time.Minute)
	b.update(bar(0, 1))

	if _, ok := b.flush(t0.Add(5*time.Minute + closeGrace - time.Second)); ok {
		t.Fatal("бар закрыт по таймеру раньше конца бара + запас")
	}
	closed, ok := b.flush(t0.Add(5*time.Minute + closeGrace))
	if !ok || !closed.Timestamp.Equal(t0) {
		t.Fatalf("бар не закрыт по таймеру: ok=%v", ok)
	}
	if _, ok := b.update(bar(0, 5)); ok {
		t.Fatal("после закрытия по таймеру бар отдан повторно")
	}
	if _, ok := b.flush(t0.Add(time.Hour)); ok {
		t.Fatal("повторное закрытие по таймеру без нового бара")
	}
}

func TestRunBarCloserОтдаётТолькоЗакрытыеБары(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	in := make(chan models.Candle)
	out := make(chan models.Candle, 8)
	go runBarCloser(ctx, "test", 5*time.Minute, in, out)

	in <- bar(0, 1)
	in <- bar(0, 2)
	in <- bar(5*time.Minute, 3)

	select {
	case c := <-out:
		if c.Close != 2 {
			t.Fatalf("отдан close=%.0f, want 2", c.Close)
		}
	case <-time.After(time.Second):
		t.Fatal("закрытый бар не дошёл до подписчика")
	}
	select {
	case c := <-out:
		t.Fatalf("формирующийся бар утёк подписчику: %+v", c)
	case <-time.After(50 * time.Millisecond):
	}
}

// Subscribe — путь стратегий — обязан идти через barCloser, а не напрямую в WS:
// иначе стратегия снова решает по формирующемуся бару (0004).
func TestSubscribeСтратегииИдётЧерезЗакрытиеБара(t *testing.T) {
	f := New(nil)
	ch := make(chan models.Candle)
	if err := f.Subscribe("CHMF", "M5", ch, nil); err != nil {
		t.Fatal(err)
	}
	key := routeKey("CHMF", "M5")
	for _, r := range f.routes[key] {
		if r.CandleChan == (chan<- models.Candle)(ch) {
			t.Fatal("канал стратегии подписан на сырой WS-поток мимо barCloser")
		}
	}
	if len(f.closers) != 1 || f.closers[0].out != (chan<- models.Candle)(ch) {
		t.Fatal("канал стратегии не подключён к выходу barCloser")
	}
}
