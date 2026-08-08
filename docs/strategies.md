# Стратегии

Архитектура стратегий и как добавить новую.  
Портфель champions: [`portfolio.md`](portfolio.md). Система: [`system.md`](system.md). Optimizer: [`optimizer-modes.md`](optimizer-modes.md).

---

## Контракт

```go
type CandleStrategy interface {
    ID() string
    OnCandle(candle models.Candle) *models.Order
}
```

- Вызов на **закрытой** свече (`candle_timeframe`, обычно M5).
- Возвращает ордер с направлением, ценой, SL/TP — или `nil`.
- Лот и исполнение — `TickerWorker` + `RiskManager`.

Стоп/тейк: `calcStopTP` / `stopDistance` в `common.go` (`stop_mode`: `range` | `atr`). R:R — `reward_ratio` или `DefaultRewardRatio`.

### Реестр

```go
Register(Descriptor{
    ID:                 "...",
    DefaultSearchSpace: "configs/strategies/....yaml",
    NewFromParams:      ...,
    ParamsToConfigFields: ...,
})
```

Создание: `strategy.NewFromParams` / `config.StrategyConfig.BuildStrategy`.

### Вокруг стратегии

Воркер: CB → лот → limit entry → trailing → SL/TP/EOD → `tradeaudit` → SQLite.  
Live: quotes между барами. Backtest: intrabar OHLC.

---

## Зарегистрированные ID

| ID | Файл | Роль в portfolio |
|---|---|---|
| `opening_range_continuation` | `opening_range_continuation.go` | Main ORC |
| `session_orc` | тот же файл (алиас) | Morning / Evening |
| `opening_range_fade` | `opening_range_fade.go` | OR Fade |
| `momentum_filtered` | `momentum_filtered.go` | MF Afternoon |
| `session_or_fade` | `opening_range_fade.go` | search space отдельно |
| `momentum_breakout` | `momentum.go` | research / не portfolio |
| `opening_range` | `opening_range.go` | research |
| `mean_reversion` | `mean_reversion.go` | research |
| `vwap_pullback_continuation`, `prev_day_level_breakout`, `midday_compression_breakout`, `afternoon_range_fade`, `late_session_imbalance`, `momentum_sber_daytrend`, `session_gap_drive` | соответствующие `.go` | research |

Search space по умолчанию — `configs/strategies/` (`DefaultSearchSpace` в `init()`).

Production paper:

```bash
go run ./cmd/bot -config configs/runs/portfolio-paper.yaml
```

Список ID: `strategy.ListIDs()` / `go test ./internal/strategy/ -run TestRegistryListsAllStrategies -v`.

---

## Как добавить стратегию

1. Константа ID в `strategy.go` (+ `DefaultRewardRatio` при необходимости).
2. Файл `internal/strategy/my_strategy.go` с `Register`, `OnCandle`, `NewFromParams`, `ParamsToConfigFields` (snake_case для YAML бота).
3. YAML search space: `configs/strategies/my-strategy.yaml` с `search_space`.
4. Тесты на сигнал / отсутствие сигнала.
5. Подключение в боте — `strategy.type` в run YAML или `experiments[]`.
6. Optimizer: `-strategy <id> -search-space configs/strategies/my-strategy.yaml`.

Параметры YAML → `Params` (`snake_case` → camelCase). Исключение: `max_trades_per_ticker_per_day` → `maxEntriesPerTickerPerDay`. Поля `trail_*` читает engine.

### Подключение в боте

Один experiment — корневые `strategy` + `tickers`.  
Несколько слотов — секция `experiments[]` (как в `portfolio-paper.yaml`): общий счёт, разные окна/тикеры.

Snapshot champion:

```bash
go run ./cmd/bot -config configs/champions/orc-wave2.yaml
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

---

## Соглашения

| Тема | Правило |
|------|---------|
| Именование | YAML snake_case; optimizer Params camelCase |
| Bool в search space | явные true/false или discrete |
| `stop_mode` vs `type` | разные оси; не путать |
| Задержка входа | session `entry_delay_minutes` и/или strategy delay |
| Whitelist | ORC/Fade могут фильтровать тикеры в коде — сверяй с YAML |
| Трейлинг | `trail_activation_r` дальше TP → trail не включится (`TRAIL_DEAD` в audit) |
