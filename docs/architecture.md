# Архитектура: карта сущностей

Кто есть кто в коде и как это собирается при старте.
Граница движок/стратегия — отдельный ADR: [`0001-engine-strategy-boundary.md`](0001-engine-strategy-boundary.md).
Риск и жизненный цикл сделки: [`system.md`](system.md). Стратегии: [`strategies.md`](strategies.md).

---

## Две зоны

| Зона | Каталог | Меняется |
|------|---------|----------|
| **Каркас (фреймворк)** | `internal/engine/**` | редко |
| **Стратегии** | `internal/strategy/**` | часто |
| **Сборка (composition root)** | `internal/app` | при добавлении сущностей каркаса |

`cmd/bot/main.go` — 36 строк, оглавление: каждая строка вызывает одну функцию `internal/app`.

---

## Сборка при старте

```
cmd/bot/main.go
   │  по порядку вызывает internal/app:
   ▼
┌──────────────────────────────────────────────────────────────────────────┐
│ internal/app — COMPOSITION ROOT (единственное место сборки)               │
│                                                                          │
│  ParseFlags ───────────────► Options            флаги CLI, LOG_FILE       │
│  InitLogging                                                              │
│  config.Load ──────────────► *config.Config     YAML: experiments[],      │
│                                                 risk.*, session.*,        │
│                                                 tickers, storage          │
│  MustConnectBroker ────────► broker.BCSClient    ◄── BCS Trade API        │
│                                                 OAuth2, WS (свечи+квоты), │
│                                                 real-ордера               │
│                                                                          │
│  BuildDependencies ───────► app.Dependencies     ОБЩАЯ ИНФРА СЧЁТА:       │
│     ├─ Executor  : contract.OrderExecutor                                 │
│     │      = execution.VirtualExecutor (paper) | broker.BCSClient (real)  │
│     ├─ Store     : contract.TradeStore = storage/sqlite | NoopTradeStore  │
│     ├─ Reader    : contract.TradeReader          (для админки)            │
│     ├─ Portfolio : *risk.GlobalRiskController    ЕДИНЫЙ СЧЁТ:             │
│     │              circuit breaker 2%/день, риск-бюджет открытых          │
│     │              позиций, one-position-per-ticker                       │
│     └─ RunID                                                              │
│                                                                          │
│  BuildTrader ─────────────► app.Trader                                    │
│     ├─ feed : *datafeed.Feed   ОДНА WS-подписка на BCS,                   │
│     │                          fan-out по (ticker, timeframe)             │
│     ├─ hub  : *dashboard.Hub   буфер свечей/цен + снапшоты позиций        │
│     └─ runners : []*engine.StrategyRunner — по одному на пару             │
│                  (experiment × ticker)                                    │
│                                                                          │
│  StartDashboard ─────────► dashboard.Server   HTTP UI/API админки (:8091) │
│  trader.Run ─────────────► запускает раннеры + стрим feed; блокируется    │
└──────────────────────────────────────────────────────────────────────────┘
```

> `app.Dependencies` — это **только общая инфраструктура счёта** (исполнитель,
> хранилище, портфельный риск), а не полный список акторов. Полная картина —
> весь пакет `internal/app`: `Dependencies` + `Trader` (feed, hub, runners) +
> то, что `BuildTrader` создаёт на каждую пару (ниже).

---

## На каждую пару (experiment × ticker)

`BuildTrader` в цикле по `experiments[] × tickers[]` создаёт:

```
engine.SessionClock           окно сессии, EOD, entry_delay_minutes
     │
strategy.CandleStrategy       СИГНАЛ («мозг»): opening_range_continuation /
     │                        opening_range_fade / momentum_filtered / …
     │   OnCandle(candle) → *models.Order | nil       (на закрытой свече)
     ▼
selfmanaged.SelfManagedStrategy      реализует contract.Strategy
     │   сайзинг в риск-бюджете · limit entry · SL/TP/трейлинг ·
     │   same-bar exit после fill · EOD · tradeaudit · запись ClosedTrade ·
     │   снапшот позиции для dashboard.Hub (contract.PositionSource)
     ▼
engine.StrategyRunner         каркасный клей:
     │   строит strategyContext = { Candles/Ticks каналы,
     │                              Orders = Executor,
     │                              Risk   = Portfolio (урезанный до RiskPort),
     │                              Trades = Store }
     │   Start(ctx): go globalDailyResetLoop(); strategy.Run(ctx, sctx)
     ▼
datafeed.Feed.Subscribe(ticker, timeframe, candleCh, tickCh)
```

Отдельный consumer той же пары `(ticker, timeframe)` через fan-out `datafeed.Feed`
кладёт свечи/тики в `dashboard.Hub` — для `/positions`, `/candles`, `/chart`.

---

## Граница движок ↔ стратегия

`internal/engine/contract` — leaf-пакет (зависит только от `internal/models`),
поэтому его импортируют и `engine`, и `strategy`, без циклов.

| Тип | Роль |
|-----|------|
| `Strategy` (`ID`, `Run(ctx, sctx)`) | самодостаточная стратегия — владеет своим торговым циклом |
| `StrategyContext` | всё, что каркас даёт стратегии: `Candles`/`Ticks`/`Timeframe`/`Orders`/`Risk`/`Trades` |
| `OrderPort` | `ExecuteOrder` / `GetBalance` (форма совпадает с `OrderExecutor`) |
| `RiskPort` | подмножество `GlobalRiskController`: `TryOpen` / `AdjustOpenRisk` / `RegisterClose` / `ReleaseOpen`. Снять circuit breaker или поднять себе бюджет стратегия **не может** |
| `TradeRecorder` | `SaveClosedTrade` — схема `models.ClosedTrade` обязательна для всех стратегий (сравнимость `expectancy_r`) |
| `OrderExecutor` / `TradeStore` / `TradeReader` / `PositionSource` | инфра-интерфейсы каркаса |

---

## Роли сущностей

| Сущность | Пакет | Роль |
|----------|-------|------|
| `BCSClient` | `engine/broker` | единственный выход наружу: OAuth2, WS-данные, real-ордера |
| `datafeed.Feed` | `engine/datafeed` | одна WS-подписка → fan-out каналов по `(ticker, timeframe)` |
| `GlobalRiskController` | `engine/risk` | единый счёт: circuit breaker, риск-бюджет открытых позиций, one-position-per-ticker; финальное «можно открыться» |
| `VirtualExecutor` | `engine/execution` | paper-исполнитель (симуляция fill) |
| `TradeStore` (sqlite) | `engine/storage/sqlite` | запись `closed_trades` |
| `SessionClock` | `engine` | торговое окно, EOD, задержки входа |
| `CandleStrategy` | `strategy` | только сигнал входа (`OnCandle`) + реестр стратегий |
| `SelfManagedStrategy` | `strategy/selfmanaged` | весь торговый цикл после сигнала (сайзинг/SL/TP/трейлинг/EOD/audit/запись) |
| `StrategyRunner` | `engine` | связывает одну `Strategy` с каркасом, запускает `Run`, гоняет дневной сброс портфельного риска |
| `dashboard.Hub` / `dashboard.Server` + `engine/api` | `engine/dashboard`, `engine/api` | админка: позиции, свечи, `/export`, архивы |
| `marketdata` | `engine/marketdata` | загрузка истории (CSV для optimizer) |
| `Dependencies` / `Trader` | `app` | composition root: собирает всё вышеперечисленное |
| `backtest` | `internal/backtest` | тот же торговый цикл на CSV (optimizer / portfolio-backtest) |

---

## Граф импортов

Линеен, без циклов:

```
models ← engine/contract ← engine/* ← strategy/selfmanaged ← app ← cmd/bot
```
