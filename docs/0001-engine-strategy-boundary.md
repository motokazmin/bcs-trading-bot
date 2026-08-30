# ADR 0001 — граница движок/стратегия

Статус: принято, реализовано.
Контекст: разделение торгового цикла на **каркас** (данные, исполнение,
портфельный риск, хранилище) и **стратегию** (сигнал + ведение позиции).

Карта всех сущностей и их роли (не только граница) — [`architecture.md`](architecture.md).

Приоритеты (не меняются):
1. Свобода стратегии (таймфрейм, объём, внутренняя архитектура).
2. 90% времени разработки — только код стратегии, не движка.
3. Интуитивность важнее чистоты границы.
4. Разделение движок/pkg — жертвуем, если мешает.

## Решение 1 — что остаётся в каркасе безусловно

Единственное, что стратегия не может обойти или переопределить:

- **DataFeed** (`internal/engine/datafeed`) — подписка на поток свечей/тиков по
  `(ticker, timeframe)`. Стратегия получает канал, но не владеет
  подключением к бирже/WS.
- **OrderExecutor** (`internal/engine/contract/executor.go`) — `ExecuteOrder` / `GetBalance`.
- **GlobalRiskController** (`internal/engine/risk/global.go`) — circuit breaker,
  капитал, лимит открытого риска на счёте. Финальное решение «можно ли
  открыться» всегда за контроллером. Стратегии он виден только через узкий
  `strategy.RiskPort` (без `ResetDaily` — снять circuit breaker стратегия не
  может; дневной сброс делает каркас, `StrategyRunner.globalDailyResetLoop`).
- **TradeStore / схема `ClosedTrade`** — см. Решение 3.

Всё между сигналом на вход и `ExecuteOrder` — юрисдикция стратегии: сайзинг
в выданном риск-бюджете, SL/TP/трейлинг, число и момент частичных закрытий,
внутренняя state machine.

## Решение 2 — `one-position-per-ticker` при разных таймфреймах

Блокировка «одна позиция на тикер» — **per-тикер на весь портфель**, не per
`(тикер, стратегия)`. Если по SBER уже открыта позиция (любой стратегии),
вторая по нему не откроется, независимо от таймфрейма.

Обоснование: самый простой и предсказуемый вариант (приоритет 3). Более
гранулярная блокировка добавляет реальную сложность (чья позиция «главнее»
при пересечении сигналов) ради сценария, которого нет ни у одного champion.
Появится конкретный кейс — пересмотрим отдельным ADR.

## Решение 3 — `ClosedTrade` расширяется под несколько leg-ов

Плоская схема (один вход/один выход) не вмещает partial exits без потери
информации. Решение — расширять схему сразу, не через «адаптер
совместимости»:

- `ClosedTrade` получает `Legs []TradeLeg` (или таблица `trade_legs` с FK на
  `trade_id`): у каждого leg свой `ExitPrice`, `Quantity`, `ClosedAt`, `CloseReason`.
- Агрегаты (`GrossPnL`, `PnLR`, `MFEinR`, `MAEinR`, `IsWinner`) остаются на
  уровне сделки (взвешенные по объёму legs) — walk-forward / оптимизатор /
  дашборд не ломаются.
- Сделки без частичных закрытий (весь текущий портфель) — вырожденный
  `len(Legs) == 1`, миграции не требуют.

> Статус: сама схема `Legs` ещё не введена — ни одна стратегия в портфеле не
> делает частичных закрытий. Вводится вместе с первой такой стратегией.

## Итоговая реализация

```
cmd/bot → internal/app (composition root)
  DataFeed (одна WS-сессия) --fan-out (ticker,timeframe)--> StrategyRunner × (experiment × ticker)
                                                              └─ SelfManagedStrategy
                                                                   ├─ CandleStrategy.OnCandle   (сигнал)
                                                                   ├─ сайзинг · limit entry · SL/TP · трейлинг · EOD
                                                                   ├─ RiskPort  → GlobalRiskController
                                                                   └─ TradeRecorder → SQLite
```

- **`internal/engine/contract/contract.go`** — `Strategy` (`ID`, `Run(ctx, sctx)`),
  `StrategyContext` (`Candles`/`Ticks`/`Timeframe`/`Orders`/`Risk`/`Trades`),
  узкие порты `OrderPort` / `RiskPort` / `TradeRecorder`.
- **`internal/engine/runner.go`** — `StrategyRunner`: собирает
  `StrategyContext` из подписанных каналов `datafeed.Feed` + `GlobalRiskController`
  + `TradeStore`/`OrderExecutor`, запускает `Strategy.Run`, гоняет
  `globalDailyResetLoop` для портфельного риска. Про SL/TP/трейлинг/EOD не знает.
- **`internal/strategy/selfmanaged/selfmanaged.go`** — `SelfManagedStrategy`:
  дженерик-обёртка любого `strategy.CandleStrategy` в самодостаточную
  `strategy.Strategy`. Ведёт позицию/SL/TP/трейлинг/EOD/сайзинг, stale-guard
  входа (`engine.CandleFreshForEntry`), `tradeaudit` (`ValidateOpen` /
  `AnnotateTrade`), ghost-handling (`ErrNoOpenPosition` → дроп; прочие ошибки
  закрытия → восстановление позиции), снапшот позиции для `dashboard.Hub`
  (`contract.PositionSource`). Переиспользует `internal/engine/position` +
  `internal/engine/trailing` как библиотеку.
  - `SessionClock` — локальный узкий интерфейс в пакете `selfmanaged`
    (минимальный контракт на стороне потребителя); `*engine.SessionClock`
    удовлетворяет ему структурно.
- **`internal/app`** — composition root: разбор флагов, поднятие брокера/
  хранилища/риск-контроллера/исполнителя (`dependencies.go`), сборка
  `Trader` из пар experiment×ticker (`trader.go`), HTTP-админка
  (`dashboard.go`). `cmd/bot/main.go` — 36 строк-оглавление.
- **`internal/engine/risk/global.go`** (Решение 1) — лимит портфеля = суммарный
  открытый риск в рублях, не число позиций:
  `maxOpenRiskBudget = deposit × risk_per_trade_percent/100 × max_parallel_trades`.
  `max_parallel_trades` в YAML сохранено — задаёт размер бюджета. Блокировка
  per-ticker (`ErrTickerBusy`) без изменений.

Никакого поля `runtime` в конфиге нет: все эксперименты идут через
`StrategyRunner` / `SelfManagedStrategy`. Как добавить свою стратегию —
[`strategies.md`](strategies.md).

## Структура каталогов (v2)

Две зоны верхнего уровня: открыл `internal/` — сразу видно, где каркас, а
где стратегии.

```
internal/
  engine/              ← ФРЕЙМВОРК (меняется редко)
    contract/          ← граница движок↔стратегия (leaf: только models)
    runner.go session.go candle_fresh.go smoke.go
    broker/ execution/ datafeed/ marketdata/
    risk/ position/ trailing/ tradeaudit/ costs/
    storage/ dashboard/ api/
  strategy/            ← СТРАТЕГИИ (меняется часто)
    candlestrategy.go registry.go + сигнальные *.go
    selfmanaged/       ← SelfManagedStrategy
  app/                 ← composition root (флаги, зависимости, Trader)
  config/ export/ optimizer/ backtest/
  models/ logx/
```

Граф импортов линеен, без циклов:
`models ← engine/contract ← engine/* ← strategy/selfmanaged ← app ← cmd/bot`.

## Что пока не решается

- Схема `Legs` для partial exits (Решение 3) — вводится с первой стратегией,
  которой она нужна.
- Пакетная граница `pkg/strategysdk` — по факту необходимости.
- Нативный H1 по WebSocket vs локальная агрегация из M5 — вопрос BCS API,
  не архитектуры.
