# Стратегии

Архитектура стратегий и как добавить новую.
Портфель champions: [`portfolio.md`](portfolio.md). Система: [`system.md`](system.md). Optimizer: [`optimizer-modes.md`](optimizer-modes.md). Граница движок/стратегия: [`0001-engine-strategy-boundary.md`](0001-engine-strategy-boundary.md).

---

## Два уровня

| Уровень | Интерфейс | Кто реализует |
|---|---|---|
| Сигнал | `strategy.CandleStrategy` (`ID`, `OnCandle`) | **ты** — файл в `internal/strategy/` |
| Самодостаточная стратегия | `contract.Strategy` (`ID`, `Run`) | `selfmanaged.SelfManagedStrategy` (`internal/strategy/selfmanaged`) — обёртка, писать не нужно |
| Каркас | `engine.StrategyRunner` | движок: даёт данные, `OrderExecutor`, портфельный риск, `TradeStore` |

Почти всегда добавляют только **сигнал** (`CandleStrategy`). `SelfManagedStrategy`
(`internal/strategy/selfmanaged`) сама берёт этот сигнал и ведёт позицию:
сайзинг в риск-бюджете, limit-entry, SL/TP/трейлинг, same-bar exit, EOD,
`tradeaudit`, запись `ClosedTrade` в SQLite. Своя `strategy.Strategy` нужна
только если стратегии не хватает модели «один сигнал на вход → одна позиция»
(например, свои частичные закрытия или несколько параллельных позиций) — тогда
см. ADR 0001.

```go
type CandleStrategy interface {
    ID() string
    OnCandle(candle models.Candle) *models.Order   // ордер (dir, price, SL, TP) или nil
}
```

- `OnCandle` — на **закрытой** свече (`candle_timeframe`, обычно M5).
- Индикаторный прогрев — внутри стратегии (`candleBuffer`, `len(history) < lookback → nil`).
- Стоп/тейк: `calcStopTP` / `stopDistance` в `common.go` (`stop_mode`: `range` | `atr`).
  R:R — `reward_ratio` из конфига или `DefaultRewardRatio(typeID)`.

### Что делает адаптер вокруг `OnCandle`

```
свежесть бара (3×TF) → session.EntriesAllowed → лимит сделок/день
  → OnCandle → circuit breaker → сайзинг (риск 0,5%) → cap по свободному кэшу
  → GlobalRisk.TryOpen (портфельный риск-бюджет + one-position-per-ticker)
  → limit entry → same-bar exit после fill
тики между барами → трейлинг + SL/TP по уровню
eod_close_time → принудительное закрытие
закрытие → tradeaudit (ValidateOpen/Close) → ClosedTrade в SQLite
```

Live: SL/TP по котировкам (`quotes`). Backtest (`internal/backtest`): intrabar OHLC.

---

## Реестр

Каждая стратегия регистрируется в `init()` своего файла:

```go
func init() {
    Register(Descriptor{
        ID:                   IDMyStrategy,                          // strategy.go
        DefaultSearchSpace:   "configs/strategies/my-strategy.yaml",
        NewFromParams:        newMyStrategyFromParams,               // Params → CandleStrategy
        ParamsToConfigFields: myStrategyConfigFields,                // Params → YAML-поля (snapshot champion)
    })
}
```

- Бот: `config.StrategyConfig.BuildStrategy(session)` → `strategy.NewFromParams(type, params, ctx)`.
- Optimizer: `strategy.NewFromParams` напрямую из search-space.
- Список ID: `strategy.ListIDs()` / `go test ./internal/strategy/ -run TestRegistryListsAllStrategies -v`.

---

## Зарегистрированные ID

| ID | Файл | Роль в portfolio |
|---|---|---|
| `opening_range_continuation` | `opening_range_continuation.go` | Main ORC / ORC Complement |
| `session_orc` | тот же файл (алиас) | Morning / Evening |
| `opening_range_fade` | `opening_range_fade.go` | OR Fade |
| `momentum_filtered` | `momentum_filtered.go` | MF Afternoon |
| `session_or_fade` | `opening_range_fade.go` | search space отдельно |
| `momentum_breakout` | `momentum.go` | research / не portfolio |
| `opening_range` | `opening_range.go` | research |
| `mean_reversion` | `mean_reversion.go` | research |
| `vwap_pullback_continuation`, `prev_day_level_breakout`, `midday_compression_breakout`, `afternoon_range_fade`, `late_session_imbalance`, `momentum_sber_daytrend`, `session_gap_drive`, `random_entry` | соответствующие `.go` | research |

---

## Как добавить стратегию

1. **ID** — константа `IDMyStrategy` в `internal/strategy/candlestrategy.go`
   (+ ветка в `DefaultRewardRatio`, если нужен свой дефолтный R:R).
2. **Сигнал** — `internal/strategy/my_strategy.go`:
   - тип со `state` (обычно `candleBuffer`), методы `ID()` и `OnCandle()`;
   - `newMyStrategyFromParams(params, ctx) (CandleStrategy, error)` — читает `params`
     (camelCase, см. `snake_case → camelCase` ниже);
   - `myStrategyConfigFields(params, ctx) map[string]interface{}` — обратная
     проекция в YAML-поля (snake_case) для снапшота champion;
   - `init()` с `Register(Descriptor{...})`.
3. **Search space** — `configs/strategies/my-strategy.yaml` c секцией `search_space`
   (диапазоны/discrete по каждому параметру).
4. **Тесты** — `internal/strategy/my_strategy_test.go`: есть сигнал на нужном
   паттерне, нет — на шуме; прогрев (`< lookback` → `nil`).
5. **Запуск в боте** — run YAML: корневой `strategy:` (одиночный) или слот в
   `experiments[]` (портфель). Поля: `type`, `stop_mode`, `reward_ratio`,
   `lookback`, `trail_*`, `max_trades_per_ticker_per_day` + свои параметры.
6. **Optimizer** — `bin/optimizer run -strategy my_strategy -search-space configs/strategies/my-strategy.yaml -tickers-config configs/shared/tickers-*.yaml`.

Никакого поля `runtime` в конфиге нет — все стратегии идут через
`StrategyRunner`/`SelfManagedStrategy`.

### Параметры: YAML ↔ optimizer

| Слой | Регистр | Пример |
|---|---|---|
| run YAML / search space | `snake_case` | `trend_sma_period: 37` |
| `strategy.Params` (код стратегии) | `camelCase` | `params["trendSmaPeriod"]` |

Маппинг — `paramKeyForYAML` (`internal/config`). Исключение:
`max_trades_per_ticker_per_day` → `maxEntriesPerTickerPerDay`.
`stop_mode` идёт в `BuildContext`, а не в `Params`; `trail_*` — не сигнальные
параметры, их читает адаптер (`trailing.Apply`).

### Подключение в боте

Один эксперимент — корневые `strategy` + `tickers`:

```bash
go run ./cmd/bot -config configs/champions/orc-wave2.yaml
```

Портфель — секция `experiments[]` (как в `portfolio-paper.yaml`): один счёт
(депозит, CB, one-position-per-ticker), у каждого слота свои окно/тикеры/params.

```bash
go run ./cmd/bot -config configs/runs/portfolio-paper.yaml   # = make bot
```

### Optimizer

```bash
make optimizer-orc
bin/optimizer run -strategy opening_range_continuation \
  -search-space configs/strategies/orc.yaml \
  -tickers-config configs/shared/tickers-orc.yaml
bin/optimizer backtest ...
bin/optimizer portfolio-backtest -config configs/runs/portfolio-paper.yaml
```

`best-config` **не** автодеплоится — снапшот в `configs/champions/` вручную.

---

## Соглашения

| Тема | Правило |
|------|---------|
| Именование | YAML snake_case; `strategy.Params` camelCase |
| Bool в search space | явные true/false или discrete |
| `stop_mode` vs `type` | разные оси; не путать |
| Задержка входа | session `entry_delay_minutes` и/или strategy `strategy_entry_delay_minutes` |
| Whitelist | ORC/Fade могут фильтровать тикеры в коде — сверяй с YAML (`allow_all_tickers`) |
| Трейлинг | `trail_activation_r` дальше TP → trail не включится (`TRAIL_DEAD` в audit) |
| Прогрев | не входить, пока `len(history) < lookback` — вернуть `nil` |
