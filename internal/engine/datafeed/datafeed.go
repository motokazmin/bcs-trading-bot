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

	"bcs-trading-bot/internal/engine"
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
	closers []closerRoute
	started bool
}

// closerRoute — подписчик на закрытые бары: WS пишет обновления в raw,
// runBarCloser перекладывает закрытые бары в out.
type closerRoute struct {
	label string
	tf    string
	raw   chan models.Candle
	out   chan<- models.Candle
}

// New создаёт DataFeed поверх уже сконфигурированного BCSClient
// (SetClassCode и т.д. должны быть вызваны до этого, как и раньше).
func New(client *broker.BCSClient) *Feed {
	return &Feed{
		client: client,
		routes: make(map[broker.RouteKey][]broker.WorkerRoutes),
	}
}

// Subscribe регистрирует маршрут для (ticker, timeframe): в candleIn приходят
// только ЗАКРЫТЫЕ бары (см. barCloser), тики — как есть. Это единственный
// режим для стратегий: backtest кормит их закрытыми барами, и live обязан
// так же (docs/analysis/0004-live-decides-on-forming-bar.md).
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

	raw := make(chan models.Candle, 64)
	if err := f.subscribe(ticker, timeframe, raw, tickIn); err != nil {
		return err
	}
	f.mu.Lock()
	f.closers = append(f.closers, closerRoute{
		label: ticker + "/" + timeframe,
		tf:    timeframe,
		raw:   raw,
		out:   candleIn,
	})
	f.mu.Unlock()
	return nil
}

// SubscribeForming — как Subscribe, но в candleIn идёт каждое обновление
// формирующегося бара (метка — начало бара). Только для отображения
// (live-график админки); решения по такому потоку принимать нельзя.
func (f *Feed) SubscribeForming(ticker, timeframe string, candleIn chan<- models.Candle, tickIn chan<- models.Tick) error {
	if ticker == "" {
		return fmt.Errorf("datafeed: пустой ticker")
	}
	if timeframe == "" {
		return fmt.Errorf("datafeed: пустой timeframe для %s (нет дефолта — передайте явно)", ticker)
	}
	if candleIn == nil {
		return fmt.Errorf("datafeed: candleIn == nil для %s/%s", ticker, timeframe)
	}
	return f.subscribe(ticker, timeframe, candleIn, tickIn)
}

func (f *Feed) subscribe(ticker, timeframe string, candleIn chan<- models.Candle, tickIn chan<- models.Tick) error {
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
	closers := f.closers
	f.mu.Unlock()

	for _, cr := range closers {
		go runBarCloser(ctx, cr.label, engine.CandleBarDuration(cr.tf), cr.raw, cr.out)
	}
	return f.client.SubscribeMarketDataFanOut(ctx, routes)
}
