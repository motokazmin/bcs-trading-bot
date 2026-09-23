package datafeed

import (
	"context"
	"time"

	"bcs-trading-bot/internal/logx"
	"bcs-trading-bot/internal/models"
)

// closeGrace — сколько ждать после конца бара, прежде чем закрыть его по
// таймеру. Обычно бар закрывается раньше — первым обновлением следующего
// (на сделках 09-15…09-21 оно приходило через 1–7 с после границы). Таймер
// нужен, когда следующего бара нет: тишина в неликвиде, конец сессии.
const closeGrace = 10 * time.Second

// closeTick — период проверки таймера.
const closeTick = time.Second

// barCloser превращает поток обновлений формирующегося бара в поток закрытых
// баров. WS БКС шлёт бар много раз, пока он формируется, с меткой НАЧАЛА бара;
// стратегия же и backtest понимают метку как полный закрытый бар. Раньше
// стратегия решала по первому обновлению — заглушке из первых секунд, — и live
// торговал не то, что тестировалось (docs/analysis/0004-live-decides-on-forming-bar.md).
//
// Бар T отдаётся, когда пришла метка > T (последним обновлением T) или по
// таймеру T + barDur + closeGrace. Обновления уже отданного бара и метки
// из прошлого отбрасываются: закрытый бар не переигрывается.
type barCloser struct {
	barDur  time.Duration
	pending *models.Candle
	emitted time.Time // метка последнего отданного бара
}

func newBarCloser(barDur time.Duration) *barCloser {
	return &barCloser{barDur: barDur}
}

// update принимает очередное обновление и возвращает бар, который им закрылся.
func (b *barCloser) update(c models.Candle) (models.Candle, bool) {
	if !b.emitted.IsZero() && !c.Timestamp.After(b.emitted) {
		return models.Candle{}, false
	}
	if b.pending == nil {
		b.pending = &c
		return models.Candle{}, false
	}
	switch {
	case c.Timestamp.Equal(b.pending.Timestamp):
		b.pending = &c
		return models.Candle{}, false
	case c.Timestamp.Before(b.pending.Timestamp):
		return models.Candle{}, false
	}
	closed := *b.pending
	b.pending = &c
	b.emitted = closed.Timestamp
	return closed, true
}

// flush закрывает бар по таймеру, если его конец плюс запас уже прошли.
func (b *barCloser) flush(now time.Time) (models.Candle, bool) {
	if b.pending == nil {
		return models.Candle{}, false
	}
	if now.Before(b.pending.Timestamp.Add(b.barDur + closeGrace)) {
		return models.Candle{}, false
	}
	closed := *b.pending
	b.pending = nil
	b.emitted = closed.Timestamp
	return closed, true
}

// runBarCloser читает обновления из in и пишет закрытые бары в out до отмены ctx
// или закрытия in. Отправка в out блокирующая — как и в broker.dispatchCandle:
// свечи не теряются молча.
func runBarCloser(ctx context.Context, label string, barDur time.Duration, in <-chan models.Candle, out chan<- models.Candle) {
	b := newBarCloser(barDur)
	ticker := time.NewTicker(closeTick)
	defer ticker.Stop()

	send := func(c models.Candle) bool {
		select {
		case out <- c:
			return true
		case <-ctx.Done():
			return false
		}
	}

	for {
		select {
		case <-ctx.Done():
			return
		case c, ok := <-in:
			if !ok {
				return
			}
			if closed, ok := b.update(c); ok && !send(closed) {
				return
			}
		case now := <-ticker.C:
			if closed, ok := b.flush(now); ok {
				logx.Info("[datafeed %s] бар %s закрыт по таймеру", label, closed.Timestamp.Format(time.RFC3339))
				if !send(closed) {
					return
				}
			}
		}
	}
}
