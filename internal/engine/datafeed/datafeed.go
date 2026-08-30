// Package datafeed — единая точка подписки на рыночные данные (свечи + тики)
// для всех стратегий в процессе. Согласно ADR 0001
// (docs/0001-engine-strategy-boundary.md), DataFeed — часть каркаса:
// стратегия получает готовый канал свечей нужного (ticker, timeframe), но
// не владеет подключением к бирже.
//
// Реализация — тонкая обвязка над broker.BCSClient.SubscribeMarketDataFanOut,
// которая уже умеет мультиплексировать несколько таймфреймов на одном
// WebSocket-соединении (см. internal/engine/broker/websocket.go, RouteKey).
package datafeed

import (
	"context"
	"fmt"
	"sync"

	"bcs-trading-bot/internal/engine/broker"
	"bcs-trading-bot/internal/models"
)

// Feed собирает подписки от стратегий/подписчиков до старта и затем прогоняет
// единую WebSocket-сессию, раздавая свечи/тики в зарегистрированные каналы.
//
// Использование:
//  1. Subscribe(...) для каждого подписчика — до вызова Run.
//  2. Run(ctx) — блокирующий вызов, обычно в отдельной горутине.
//
// Регистрация подписок после Run не поддерживается — это осознанное
// ограничение первой итерации (см. Фазу 1 плана: "не проектировать заранее
// под все случаи"). Если понадобится динамическая пере-подписка на лету —
// это отдельный шаг, не текущий.
type Feed struct {
	client *broker.BCSClient

	mu      sync.Mutex
	routes  map[broker.RouteKey][]broker.WorkerRoutes
	started bool
}

// New создаёт DataFeed поверх уже сконфигурированного BCSClient
// (SetClassCode и т.д. должны быть вызваны до этого, как и раньше).
func New(client *broker.BCSClient) *Feed {
	return &Feed{
		client: client,
		routes: make(map[broker.RouteKey][]broker.WorkerRoutes),
	}
}

// Subscribe регистрирует маршрут для (ticker, timeframe): свечи/тики,
// приходящие с биржи по этой паре, будут писаться в candleIn/tickIn.
// Один и тот же (ticker, timeframe) может быть зарегистрирован несколько раз
// разными подписчиками — каждый получит свою копию потока (fan-out на уровне
// BCSClient, не на уровне Feed). tickIn может быть nil, если тики не нужны
// (например, чисто свечная стратегия без внутрибарового SL/TP).
//
// timeframe пустым быть не должно — вызывающая сторона (обычно
// config.ResolvedExperiment.CandleTimeframe) обязана подставить дефолт
// заранее; здесь мы уже ничего не знаем про глобальный конфиг.
func (f *Feed) Subscribe(ticker, timeframe string, candleIn chan<- models.Candle, tickIn chan<- models.Tick) error {
	if ticker == "" {
		return fmt.Errorf("datafeed: пустой ticker")
	}
	if timeframe == "" {
		return fmt.Errorf("datafeed: пустой timeframe для %s (нет дефолта — передайте явно)", ticker)
	}
	if candleIn == nil {
		return fmt.Errorf("datafeed: candleIn == nil для %s/%s", ticker, timeframe)
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	if f.started {
		return fmt.Errorf("datafeed: Subscribe после Run не поддерживается (тикер %s, tf %s)", ticker, timeframe)
	}

	key := broker.RouteKey{Ticker: ticker, Timeframe: timeframe}
	f.routes[key] = append(f.routes[key], broker.WorkerRoutes{
		CandleChan: candleIn,
		TickChan:   tickIn,
	})

	return nil
}

// Run запускает единую WebSocket-сессию на все зарегистрированные подписки
// и блокируется до отмены ctx или неустранимой ошибки. После вызова Run
// дальнейшие Subscribe возвращают ошибку.
func (f *Feed) Run(ctx context.Context) error {
	f.mu.Lock()
	if f.started {
		f.mu.Unlock()
		return fmt.Errorf("datafeed: Run уже вызывался")
	}
	if len(f.routes) == 0 {
		f.mu.Unlock()
		return fmt.Errorf("datafeed: нет ни одной подписки (Subscribe не вызывался)")
	}
	f.started = true
	routes := f.routes
	f.mu.Unlock()

	return f.client.SubscribeMarketDataFanOut(ctx, routes)
}
