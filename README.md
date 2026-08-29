# BCS Trading Bot

Торговый робот на Go для [BCS Trade API](https://trade-api.bcs.ru). Дейтрейдинг акциями MOEX (TQBR) на M5 с жёстким риск-менеджментом.

Paper portfolio — **6 слотов** на едином счёте 200 000 ₽: Morning/Evening Session ORC, Main ORC, ORC Complement, OR Fade, MF Afternoon.

| | |
|---|---|
| Запуск | [`docs/runbook.md`](docs/runbook.md) |
| Портфель | [`docs/portfolio.md`](docs/portfolio.md) |
| Baseline | [`docs/baseline.md`](docs/baseline.md) |
| Система | [`docs/system.md`](docs/system.md) |
| Стратегии | [`docs/strategies.md`](docs/strategies.md) |
| Optimizer | [`docs/optimizer-modes.md`](docs/optimizer-modes.md) · [`cmd/optimizer/README.md`](cmd/optimizer/README.md) |
| План | [`Roadmap.md`](Roadmap.md) |

Робот контролирует убытки: `reward_ratio` ~1,2–1,8, риск **0,5%**/сделку, Circuit Breaker **2%**/день.

| Режим | `trading_mode` | Поведение |
|---|---|---|
| Paper | `virtual` | Симуляция исполнения, деньги не тратятся |
| Real | `real` | Ордера в BCS (**сейчас — один experiment за запуск**) |

Для real нужен refresh token со скоупом `trade-api-write`. Начинайте с `virtual`.

---

## Архитектура

```
cmd/bot
  YAML → OAuth2 → OrderExecutor (virtual | real)
  WebSocket (M5 + quotes) → DataFeed (fan-out по ticker,timeframe)
      → StrategyRunner × (experiment × ticker)
          → SelfManagedStrategy: сигнал → сайзинг → limit entry → SL/TP/trail → EOD
          → GlobalRisk (риск-бюджет, CB) + TradeStore + tradeaudit
```

При секции `experiments` все слоты на **одном** счёте (общий депозит, CB, one-position-per-ticker). HTTP-админка в том же процессе (`-http-listen`).

Поток: M5 → сигнал → CB + лот → limit entry → SL/TP/trail по котировкам → SQLite.

---

## Быстрый старт

```bash
export BCS_REFRESH_TOKEN=...
make build
make bot                 # фон → http://127.0.0.1:8091
# make bot-status / make bot-stop

# облако:
# export ADMIN_TOKEN=$(openssl rand -hex 32)
# export HTTP_LISTEN=0.0.0.0:8091
# make bot
# make bot-stop

make sync-history        # CSV для optimizer
```

Подробности bind/логов: [`docs/runbook.md`](docs/runbook.md).

Smoke (OAuth + WS + virtual open/close):

```bash
make bot-smoke
```

---

## Конфиги

| Файл | Назначение |
|---|---|
| `configs/runs/portfolio-paper.yaml` | Paper: 6 слотов |
| `configs/champions/*.yaml` | Snapshot params champions |
| `configs/runs/real-stocks.yaml` | Real, один тикер/experiment |
| `configs/runs/virtual-futures.yaml` | Paper фьючерсы (не portfolio) |
| `configs/strategies/*.yaml` | Search space для optimizer |

Секреты только в env. Локальные оверрайды: `configs/local*.yaml` (gitignore).

Ключевые поля: `trading_mode`, `tickers`, `risk.*`, `strategy.type` / `reward_ratio` / `trail_*`, `experiments[]`, `storage.path`.

---

## Логи

`pkg/logx`: `[SYS]`, `[OPEN]`, `[TP]`/`[SL]`/`[EOD]`, `[TRAIL]`, `[SKIP]`, `[AUDIT]`, `[WS]`, `[ERR]`.

Метка воркера: `[TATN]` или `[orc-wave2/TATN]`. Файл по умолчанию: `/var/log/trading-bot/bot.log`.

Paper: SL/TP по уровню; после ORC limit-fill — same-bar проверка. Аномалии — `[AUDIT]` и поля `audit_*` в БД/export.

---

## Админка и экспорт

`-http-listen` (дефолт `127.0.0.1:8091`): `/`, `/open`, `/trades`, `/export`.  
Export JSON + prompt для ИИ (версия в `internal/export`).

---

## Модули

| Пакет | Роль |
|---|---|
| `internal/engine` | `StrategyRunner`, `SessionClock`, freshness бара |
| `internal/strategies/adapter` | `SelfManagedStrategy` — сигнал → позиция/SL/TP/EOD/сайзинг |
| `internal/datafeed` | Единая WS-подписка (ticker, timeframe) → fan-out |
| `internal/strategy` | Сигналы |
| `internal/position` | Состояние, fill-at-level, same-bar |
| `internal/tradeaudit` | Валидность входа/выхода |
| `internal/risk` | Лот, CB, global risk |
| `internal/simulation` | Backtest runner |
| `internal/storage/sqlite` | `closed_trades` |
| `internal/export` / `internal/live` | Выгрузка и HTTP UI |
| `cmd/bot`, `cmd/optimizer` | Бинарники |

---

## Структура

```
cmd/bot, cmd/optimizer
configs/runs, configs/champions, configs/strategies, configs/shared
docs/
internal/...
data/history, data/trades.db   # runtime; history CSV локально
results/                       # выход optimizer, gitignore
```
