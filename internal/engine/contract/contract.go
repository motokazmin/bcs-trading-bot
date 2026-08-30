// Package contract — граница движок ↔ стратегия (ADR 0001).
//
// Здесь и только здесь описано всё, что каркас (internal/engine) обещает
// стратегии (internal/strategy) и наоборот: сам контракт Strategy/
// StrategyContext, узкие порты (OrderPort/RiskPort/TradeRecorder) и
// инфраструктурные интерфейсы каркаса (OrderExecutor, TradeStore/TradeReader,
// PositionSource). Пакет — leaf: зависит только от internal/models, поэтому
// его свободно импортируют и engine, и strategy, и брокер, без циклов.
package contract

import (
	"context"

	"bcs-trading-bot/internal/models"
)

// Strategy — самодостаточная торговая стратегия (ADR 0001,
// docs/0001-engine-strategy-boundary.md). В отличие от CandleStrategy
// (только сигнал на вход), Strategy сама владеет своим торговым циклом:
// подписывается на данные через StrategyContext, сама решает
// сайзинг/выход/трейлинг/частичные закрытия. Единственное, что она обязана
// делать через каркас — запрашивать риск (RiskPort) и сохранять закрытые
// сделки (TradeRecorder).
//
// Run блокируется до ctx.Done(). Strategy сама читает Candles()/Ticks() из
// StrategyContext в цикле — каркас ничего не диспетчеризирует за неё после
// старта.
//
// Контракт сознательно минимальный и не заморожен: по приоритетам
// рефакторинга ("интуитивность важнее чистоты", см. план) он будет
// донастраиваться по мере переноса первых 2-3 стратегий, это ожидаемо.
type Strategy interface {
	ID() string
	Run(ctx context.Context, sctx StrategyContext)
}

// StrategyContext — всё, что каркас предоставляет стратегии. Стратегия не
// получает доступ ни к чему за пределами этого интерфейса (никакого прямого
// доступа к BCSClient, sqlite, другим воркерам и т.д.).
type StrategyContext interface {
	// Ticker — тикер, под который эта StrategyContext создана.
	Ticker() string
	// Candles/Ticks — потоки рыночных данных для (Ticker(), Timeframe()),
	// уже подписанные через internal/engine/datafeed. Закрываются при остановке
	// процесса.
	Candles() <-chan models.Candle
	Ticks() <-chan models.Tick
	// Timeframe — таймфрейм, на котором подписаны Candles().
	Timeframe() string

	// Orders — исполнение ордеров (ExecuteOrder/GetBalance); та же
	// абстракция, что OrderExecutor (executor.go).
	Orders() OrderPort

	// Risk — риск-бюджет счёта. Урезанный интерфейс поверх
	// risk.GlobalRiskController: стратегия не может расширить дневной лимит
	// или обойти circuit breaker, только резервировать/освобождать риск
	// под свои позиции.
	Risk() RiskPort

	// Trades — сохранение закрытых сделок. Схема models.ClosedTrade
	// обязательна для всех стратегий вне зависимости от внутренней
	// архитектуры (Решение 3, ADR 0001) — это единственное поле, где
	// свобода стратегии искусственно ограничена, ради сравнимости exp_R
	// между чемпионами и новыми гипотезами.
	Trades() TradeRecorder
}

// OrderPort — исполнение ордеров, доступное стратегии из StrategyContext.
// Совпадает по форме с OrderExecutor (executor.go): OrderPort — имя роли в
// контракте стратегии, OrderExecutor — та же абстракция со стороны каркаса.
type OrderPort interface {
	ExecuteOrder(ctx context.Context, order models.Order) error
	GetBalance(ctx context.Context) (float64, error)
}

// RiskPort — то подмножество risk.GlobalRiskController, которое доступно
// стратегии. Финальное решение "можно ли открыться" всегда за каркасом
// (Решение 1, ADR 0001) — стратегия не может, например, поднять себе
// max_open_risk_budget.
type RiskPort interface {
	// TryOpen атомарно проверяет circuit breaker + risk-budget + занятость
	// тикера и резервирует риск под открытие. При ошибке исполнения ордера
	// вызывающий обязан ReleaseOpen.
	TryOpen(ticker string, riskAmount float64) error
	// AdjustOpenRisk уменьшает/меняет зарезервированный риск по уже
	// открытой позиции — используется при частичной фиксации прибыли, чтобы
	// освободить бюджет для новых сделок, не дожидаясь полного закрытия.
	AdjustOpenRisk(ticker string, newRiskAmount float64)
	// RegisterClose снимает резерв риска и учитывает реализованный PnL.
	RegisterClose(ticker string, pnl float64)
	// ReleaseOpen откатывает резерв без учёта PnL (неудачный ExecuteOrder).
	ReleaseOpen(ticker string)
}

// TradeRecorder — сохранение закрытых сделок. Узкий интерфейс поверх
// TradeStore (trade_store.go), без Close() — стратегия не владеет жизненным
// циклом БД.
type TradeRecorder interface {
	SaveClosedTrade(ctx context.Context, trade models.ClosedTrade) error
}
