# BCS Trading Bot (MVP)

Торговый робот на Go для [BCS Trade API](https://trade-api.bcs.ru). **Дейтрейдинг акциями MOEX** (TQBR) на 5-минутных свечах с жёстким риск-менеджментом. FROZEN portfolio — 5 champions (ORC, OR Fade, MF Afternoon, Morning/Evening Session ORC) на едином счёте 200k.

Робот не пытается «угадать» рынок — он контролирует убытки:

- асимметрия R:R через `strategy.reward_ratio` (у champions ~1,2–1,8);
- риск на сделку **0.5%** от депозита;
- суточный предохранитель **Circuit Breaker**.

Поддерживает **несколько тикеров** (каждый в своей горутине), **несколько слотов стратегий** (`experiments` в конфиге на одном счёте) и два режима торговли:

| Режим | `trading_mode` в конфиге | Поведение |
|---|---|---|
| Виртуальный (paper trading) | `virtual` | Сделки симулируются в памяти, деньги не тратятся |
| Реальный | `real` | Ордера отправляются в BCS Trade API (**сейчас — один experiment за запуск**) |

> **Важно.** Для реальной торговли нужен refresh token со скоупом `trade-api-write`. Начинайте с `trading_mode: virtual`. Portfolio на одном real-счёте — [Roadmap.md](Roadmap.md).

### Production portfolio (FROZEN)

После walk-forward исследований зафиксированы **5 champion-стратегий** на слотах торгового дня MOEX:

| Слот MSK | Стратегия | Тикеры |
|------|-----------|--------|
| 07:00–09:50 | Morning Session ORC | ROSN, NVTK, MOEX, CHMF, TATN, SBER |
| ~10:00–10:30 | ORC | MGNT, ROSN, TATN |
| ~10:15–12:30 | OR Fade | LKOH, CHMF, MOEX, AFKS |
| 12:30–18:40 | MF Afternoon | MGNT, TATN |
| 19:05–23:50 | Evening Session ORC | NVTK, GAZP, ROSN, CHMF, MOEX, TATN, MGNT |

Paper trading: `configs/runs/portfolio-paper.yaml` (единый virtual-счёт 200k). Параметры: `configs/champions/*.yaml`.

Baseline доходности (для сравнения с live): [`docs/champion-baseline.md`](docs/champion-baseline.md) § C.

Документация: [`docs/strategy-research.md`](docs/strategy-research.md) · [`docs/optimizer-modes.md`](docs/optimizer-modes.md) · [`docs/system.md`](docs/system.md) · [`docs/strategies.md`](docs/strategies.md)

---

## Содержание

- [Архитектура](#архитектура)
- [Требования](#требования)
- [Запуск](#запуск)
- [Что вы увидите в логе](#что-вы-увидите-в-логе)
- [Конфигурация](#конфигурация)
- [Веб-админка и экспорт для ИИ](#веб-админка-и-экспорт-для-ии)
- [Стратегии](docs/strategies.md)
- [Исследования и champions](docs/strategy-research.md)
- [Модули](#модули)
- [Структура проекта](#структура-проекта)
- [Дорожная карта](Roadmap.md)
- [Как устроена система](docs/system.md)

---

## Архитектура

```
┌──────────────────────────────────────────────────────────────────────────────┐
│                              cmd/bot/main.go                                 │
│                                                                              │
│  YAML-конфиг ──► OAuth2 ──► единый OrderExecutor (virtual / real)           │
│                                    ▲                                         │
│  WebSocket Fan-Out ──┬──► TickerWorker [baseline/SBER] ──► Strategy + Risk  │
│  (свечи + котировки) ├──► TickerWorker [atr-2/SBER]    ──► Strategy + Risk  │
│                      └──► TickerWorker [atr-3/GAZP]    ──► Strategy + Risk  │
└──────────────────────────────────────────────────────────────────────────────┘
```

При секции `experiments` все слоты торгуют на **одном virtual-счёте** (общий депозит, общий Circuit Breaker, one-position-per-ticker). Один WebSocket раздаёт рыночные данные всем воркерам (fan-out).

Live HTTP-админка (`internal/live`) встроена в тот же процесс `cmd/bot`: общий адресное пространство, GC и failure domain с торговым циклом. Это осознанный trade-off (проще деплой, меньше движущихся частей). Выделение админки в отдельный бинарник — осознанный откат, если понадобится изоляция процессов.

Поток данных:

1. Загрузка YAML-конфига (`-config`), токен из `BCS_REFRESH_TOKEN`.
2. Авторизация через OAuth2 Keycloak → `access_token`.
3. Инициализация исполнителя: один `VirtualExecutor` (или `BCSClient`) на весь процесс.
4. Для каждой пары «эксперимент × тикер» создаётся `TickerWorker` в отдельной горутине.
5. WebSocket подписывается на **свечи** (сигналы стратегии) и **котировки** (тики для SL/TP).
6. Воркер на каждом тике проверяет стоп/тейк и EOD; на свече — стратегию из `strategy.type` (см. [docs/strategies.md](docs/strategies.md)).
7. При сигнале риск-менеджер проверяет Circuit Breaker и считает лот.
8. Вход — лимитный ордер; закрытие (SL/TP/EOD) — рыночный ордер.

Остановка: `Ctrl+C` (SIGINT) или SIGTERM.

---

## Требования

- Go **1.21+**
- Аккаунт БКС с доступом к [Trade API](https://trade-api.bcs.ru)
- Refresh token (`BCS_REFRESH_TOKEN`)

---

## Запуск

### 1. Сборка

```bash
cd bcs-trading-bot
go build -o bot ./cmd/bot
```

Или через **Makefile** (бот, optimizer):

```bash
make help              # список команд
make build             # bin/bot, bin/optimizer
make bot               # paper trading, portfolio (5 champions); админка на http://127.0.0.1:8091
make sync-history      # догрузить CSV-историю для optimizer (9 акций)
make optimizer-orc     # ORC (FROZEN — только по запросу)
```

Переменные optimizer: `OPTIMIZER_PARALLEL`, `OPTIMIZER_TWO_PHASE=1`, `SEARCH_SPACE`. Документация: [`cmd/optimizer/README.md`](cmd/optimizer/README.md) · режимы solo/portfolio: [`docs/optimizer-modes.md`](docs/optimizer-modes.md).

Требуется `export BCS_REFRESH_TOKEN=...` (см. ниже). Для публичного HTTP-доступа к админке — ещё `ADMIN_TOKEN`.

### 2. Токены окружения

```bash
export BCS_REFRESH_TOKEN="ваш_refresh_token"
# опционально: защита HTTP-админки (обязателен при -http-listen не на localhost)
export ADMIN_TOKEN="случайная_строка"
```

Токены **не хранятся в конфиге** — только в env.

### 3. Paper trading — portfolio (рекомендуется)

```bash
go run ./cmd/bot -config configs/runs/portfolio-paper.yaml
# или
make bot
```

5 FROZEN champions на едином счёте 200k (10 тикеров universe, слоты 07:00–23:50), один WebSocket. Сделки пишутся в `data/trades.db` с полем `experiment_id`.

### 3a. Paper trading — legacy A/B (архив)

```bash
go run ./cmd/bot -config configs/runs/legacy/experiments-all.yaml
```

Исторический пакет momentum/OR (отдельные депозиты на experiment — устаревшая модель). См. `configs/runs/legacy/`.

### 4. Paper trading — фьючерсы (legacy, не champions)

```bash
go run ./cmd/bot -config configs/runs/virtual-futures.yaml
# или
make bot-futures
```

Ранний MVP-путь; все optimizer-прогоны и FROZEN portfolio — **только акции TQBR**.

### 5. Реальная торговля

```bash
# нужен refresh token со скоупом trade-api-write
go run ./cmd/bot -config configs/runs/real-stocks.yaml
# или
make bot-real
```

### 5a. Быстрая проверка перед сессией (smoke-test)

Проверяет OAuth, WebSocket и виртуальное исполнение: один цикл open/close на первой котировке, без записи в БД:

```bash
go run ./cmd/bot -config configs/runs/portfolio-paper.yaml -smoke-test
```

Бот ждёт первую котировку по первому тикеру из конфига, открывает и сразу закрывает 1 лот в `VirtualExecutor`, затем завершается. Работает только с `trading_mode: virtual`. При успехе в логе:

```
Smoke-test: OK — OAuth, WebSocket и виртуальное исполнение работают
```

После этого запускайте основную сессию без флага `-smoke-test`.

### 8. Локальный конфиг

Скопируйте профиль и настройте под себя (файлы `configs/local*.yaml` в `.gitignore`):

```bash
cp configs/runs/portfolio-paper.yaml configs/local.yaml
# отредактируйте configs/local.yaml
go run ./cmd/bot -config configs/local.yaml
```

Флаг `-no-color` отключает ANSI-раскраску в терминале.

### Устаревший способ (env-переменные)

Раньше все настройки задавались через env. Теперь используйте YAML-конфиги в `configs/`.

```bash
export BCS_REFRESH_TOKEN="ваш_refresh_token"
export TRADING_MODE="virtual"
export BCS_TICKER="SBER"

go run ./cmd/bot
```

---

## Что вы увидите в логе

Логи выводятся через пакет `pkg/logx`: время `ЧЧ:ММ:СС`, цветные теги, единый формат для **virtual** и **real** (исполнитель в лог не пишет — только воркер).

### Теги

| Тег | Значение |
|---|---|
| `[SYS]` | Старт, конфиг, шаги инициализации |
| `[MODE]` | Режим торговли и баланс единого virtual-счёта |
| `[WS]` | WebSocket: подписка, реконнект, ошибки сервера |
| `[OPEN]` | Позиция открыта |
| `[TP]` / `[SL]` / `[EOD]` | Позиция закрыта по тейку, стопу или концу дня |
| `[TRAIL]` | Трейлинг-стоп подтянул SL (+1R / +2R / непрерывный MFE−1R) |
| `[SKIP]` | Сигнал отклонён риск-менеджером |
| `[ERR]` | Ошибка исполнения или сохранения |
| `[WARN]` | Предупреждение (не фатально) |

Метка воркера: `[SBER]` (один эксперимент) или `[atr-3/TATN]` (эксперимент + тикер).

### Старт бота (виртуальный режим, несколько экспериментов)

```
10:00:00 [SYS] Запуск торгового робота БКС на Go...
10:00:00 [SYS] Конфиг: configs/runs/portfolio-paper.yaml | Режим: virtual | Тикеры: MGNT, ROSN, ... | Эксперименты: orc-wave2 (...), ... | Класс: TQBR | Свечи: M5
10:00:00 [SYS] Шаг 1: Авторизация через БКС OAuth...
10:00:01 [SYS] Access Token получен.
10:00:01 [SYS] Хранилище сделок: data/trades.db
10:00:01 [MODE] virtual [atr-2-lean] баланс 200000 руб.
10:00:01 [MODE] virtual [atr-1-lean] баланс 200000 руб.
10:00:01 [atr-2-lean/SBER] воркер запущен
10:00:01 [atr-1-lean/SBER] воркер запущен
...
10:00:01 [SYS] Шаг 2: Запущено 46 воркеров (6 экспериментов, EOD: 18:40 МСК)
10:00:01 [SYS] Шаг 3: Торговый цикл активен. Мониторинг SL/TP и EOD включён.
10:00:02 [WS] подписка 10 инструмент(ов) [SBER GAZP ...] — свечи M5 + котировки
```

После старта бот принимает свечи WebSocket. Первые входы зависят от стратегии и сессии:

| Стратегия (FROZEN) | Когда возможны входы |
|---|---|
| Session ORC (morning/evening) | После `orb_minutes` от `session_open_time` эксперимента (напр. morning: OR ~14 мин с 07:00) |
| ORC / OR Fade (main) | После `orb_minutes` от открытия слота (обычно с 10:00) |
| MF Afternoon | С ~12:30 (`entry_delay_minutes`) и при накопленном `lookback` в буфере |

`strategy.lookback` — окно баров для momentum/MF (пробой high/low). У ORC/Fade вход задаётся `orb_minutes`.

В `session.session_open_time` (по умолчанию 10:00 МСК) сбрасывается дневной лимит убытков:

```
10:00:00 [baseline/SBER] новый торговый день: дневной счётчик убытков сброшен
```

### Старт бота (реальный режим)

```
10:00:00 [SYS] Запуск торгового робота БКС на Go...
10:00:00 [SYS] Конфиг: configs/runs/real-stocks.yaml | Режим: real | Тикеры: SBER | Эксперименты: default | ...
10:00:01 [MODE] REAL BCSClient (trade-api-write)
10:00:02 [SYS] Баланс счёта: 1543280.50 руб.
10:00:02 [SBER] воркер запущен
10:00:02 [SYS] Шаг 2: Запущено 1 воркеров (1 экспериментов × 1 тикеров, EOD: 18:40 МСК)
10:00:02 [WS] подписка 1 инструмент(ов) [SBER] — свечи M5 + котировки
```

### Открытие позиции

Одна строка на сделку (virtual и real одинаково):

```
16:05:00 [atr-3/TATN] [OPEN] BUY x4 @ 501.00 SL=479.68 TP=564.96
16:05:00 [baseline/TATN] [OPEN] BUY x39 @ 501.00 SL=498.50 TP=508.51
16:05:00 [atr-2/TATN] [OPEN] BUY x7 @ 501.00 SL=486.79 TP=543.64
```

При одном пробое на тикере все эксперименты входят одновременно, но с **разным объёмом и стопами** (разные `stop_mode` / `atr_multiplier`).

### Сопровождение и закрытие

```
16:25:00 [atr-3/TATN] [TRAIL] +1R → SL=501.00
16:30:00 [atr-3/TATN] [TRAIL] +2R → SL=522.32
17:10:00 [atr-3/TATN] [TP] @ 564.96 PnL=+255.86 +3.00R
16:42:15 [baseline/SBER] [SL] @ 248.75 PnL=-50.00 -1.00R
18:40:00 [atr-3/TATN] [EOD] @ 498.50 PnL=-10.00 -0.12R
```

| Поле в строке закрытия | Смысл |
|---|---|
| `@ 564.96` | Цена выхода |
| `PnL=+255.86` | Результат в **рублях** (gross, без комиссии брокера) |
| `+3.00R` | Результат в **единицах риска** R: `PnL / (расстояние до стопа × лоты × step_price_value)`. Тейк при 1:3 ≈ `+3.00R`, стоп ≈ `-1.00R` |

### Отклонённые сигналы и ошибки

```
12:00:00 [GAZP] [SKIP] BUY risk manager: дневной лимит убытков превышен, торговля заблокирована
12:05:00 [SBER] [SKIP] SELL нулевой объём позиции
12:10:00 [ERR] [baseline/SBER] ошибка исполнения ордера: [VIRTUAL] недостаточно средств: нужно 500000.00, доступно 90000.00
```

`[SKIP]` — сигнал был, риск не пустил. `[ERR]` после попытки исполнения — например, не хватило виртуального баланса на счёте эксперимента (все тикеры эксперимента делят один `virtual.balance`).

### Обрыв WebSocket (автоматический реконнект)

```
13:00:00 [WS] рыночные данные: ошибка чтения: ..., переподключение через 1s
13:00:02 [WS] подписка 10 инструмент(ов) [SBER GAZP ...] — свечи M5 + котировки
```

Задержка растёт экспоненциально: 1 с → 2 с → 4 с → … → 60 с.

### Остановка

```
^C
18:00:00 [baseline/SBER] воркер остановлен
18:00:00 [SYS] Завершение работы...
```

### Критические ошибки (бот завершается)

```
[ERR] задайте переменную окружения BCS_REFRESH_TOKEN
[ERR] ошибка загрузки конфига: ...
[ERR] авторизация провалена: ...
[ERR] стрим рыночных данных остановлен: ...
```

---

## Конфигурация

Все настройки — в YAML-файлах каталога `configs/`. Единственный секрет — `BCS_REFRESH_TOKEN` в env.

### Готовые профили

| Файл | Назначение |
|---|---|
| `configs/runs/portfolio-paper.yaml` | **Paper trading, 5 FROZEN champions** (morning/evening Session ORC + ORC + OR Fade + MF Afternoon) |
| `configs/champions/*.yaml` | Snapshot параметров champions (solo-запуск одной стратегии) |
| `configs/runs/virtual-futures.yaml` | Legacy: paper trading, фьючерсы SPBFUT (не champions) |
| `configs/runs/real-stocks.yaml` | Реальные ордера, акции TQBR |
| `configs/runs/legacy/experiments-all.yaml` | Legacy A/B momentum (архив, multi-account) |

Запуск: `go run ./cmd/bot -config configs/runs/portfolio-paper.yaml` или `make bot`

### Поля конфига

| Поле | По умолчанию | Описание |
|---|---|---|
| `trading_mode` | `virtual` | `virtual` — paper trading, `real` — реальные ордера |
| `tickers` | — | Список тикеров |
| `class_code` | `TQBR` | Класс инструмента: `TQBR` (акции), `SPBFUT` (фьючерсы) |
| `candle_timeframe` | `M5` | Таймфрейм свечей WebSocket: M1, M5, M15, ... |
| `costs.commission_rate_per_leg` | `0.00008` (TQBR) | **0,008% за leg** — основная модель для акций |
| `costs.commission_per_lot` | `5.0` (SPBFUT) | Flat round-trip за контракт фьючерса (legacy) |
| `risk.deposit` | `100000` | Депозит для расчёта лота (руб.) |
| `risk.max_daily_loss` | — | Абсолютный дневной лимит убытков (руб.) |
| `risk.max_daily_loss_percent` | `2` | % от депозита, если `max_daily_loss` не задан |
| `risk.risk_per_trade_percent` | `0.5` | Риск на сделку (% депозита) |
| `strategy.type` | `momentum_breakout` | ID стратегии — см. [docs/strategies.md](docs/strategies.md): `opening_range_continuation`, `opening_range_fade`, `momentum_filtered`, … |
| `strategy.lookback` | `20` | Окно баров для momentum/MF (пробой high/low). У ORC/Fade вход задаётся `orb_minutes` |
| `strategy.stop_mode` | `range` | `range` — стоп от половины диапазона; `atr` — ATR × multiplier |
| `strategy.atr_period` | `14` | Период ATR (для `stop_mode: atr`) |
| `strategy.atr_multiplier` | `2.0` | Множитель ATR для ширины стопа |
| `strategy.range_use_cap` | `true` | Ограничить range-стоп cap 0.5% от цены входа |
| `strategy.max_trades_per_ticker_per_day` | `0` | Лимит входов на тикер в день (`0` — без лимита) |
| `strategy.volume_filter` | `false` | Фильтр по объёму: вход только при объёме свечи > `volume_min_ratio` × средний объём окна |
| `strategy.volume_min_ratio` | `1.5` | Множитель к среднему объёму (при `volume_filter: true`) |
| `virtual.balance` | = `risk.deposit` | Стартовый баланс единого virtual-счёта |
| `storage.path` | `data/trades.db` | SQLite с закрытыми сделками |
| `experiments[]` | — | Слоты стратегий в одном процессе на общем счёте (`real`: пока **1** experiment) |
| `experiments[].id` | — | Идентификатор (`experiment_id` в БД и в логе `id/ticker`) |
| `experiments[].tickers` | корневой `tickers` | Подмножество тикеров для этого эксперимента |
| `experiments[].entry_delay_minutes` | корневой `session.entry_delay_minutes` | Задержка входов для эксперимента (минуты) |
| `session.timezone` | `Europe/Moscow` | Часовой пояс для EOD-логики |
| `session.eod_close_time` | `23:40` | Принудительное закрытие позиций (ЧЧ:ММ) |
| `session.session_open_time` | `10:00` | Начало торгового дня (ЧЧ:ММ); в этот момент сбрасывается дневной счётчик убытков |
| `session.entry_delay_minutes` | `0` | Задержка входов после открытия сессии (минуты); `30` → входы с 10:30 при open 10:00 |

При нескольких тикерах `risk.deposit` и `risk.max_daily_loss` **делятся поровну** между воркерами одного эксперимента. При секции `experiments` корневые `risk` / `strategy` / `virtual` игнорируются — параметры задаются внутри каждого элемента `experiments[]`.

### Получение refresh token

1. Зайдите в [trade-api.bcs.ru](https://trade-api.bcs.ru).
2. Сгенерируйте refresh token в личном кабинете API.
3. Для `trading_mode: real` убедитесь, что токен имеет скоуп **trade-api-write**.
4. Сохраните токен в переменную окружения — **не коммитьте его в git**.

---

## Веб-админка и экспорт для ИИ

Админка встроена в бот: статичный UI + HTTP API на одном порту (по умолчанию `127.0.0.1:8091`).

```bash
export BCS_REFRESH_TOKEN=...
# локально токен админки не обязателен
make bot
# открыть http://127.0.0.1:8091
```

На облачной VM с публичным IP (тот же `make bot`):

```bash
export BCS_REFRESH_TOKEN=...
export ADMIN_TOKEN=$(openssl rand -hex 32)
export HTTP_LISTEN=0.0.0.0:8091
make bot
# браузер: http://PUBLIC_IP:8091 → ввести ADMIN_TOKEN
```

Без `ADMIN_TOKEN` публичный bind (`0.0.0.0`, `:8091` и т.п.) не стартует. `GET /healthz` без авторизации; остальные API — `Authorization: Bearer <ADMIN_TOKEN>` (или `?access_token=`).

| Страница / endpoint | Назначение |
|---|---|
| `/` | Дашборд, сравнение experiment_id |
| `/open` | Открытые позиции и график |
| `/trades` | Таблица сделок |
| `/export` | Экспорт для ИИ: промпт + JSON с данными |
| `GET /api/prompt?mode=summary\|detailed` | Текст промпта для копирования |
| `GET /api/export/data?mode=summary\|detailed` | JSON с данными (`data-summary.json` / `data-trades.json`) |

Флаги: `-http-listen` (пустая строка — выключить HTTP), `-archives` (по умолчанию `data/archives.json`).

### Как анализировать с ИИ

1. Накопите сделки ботом (`make bot` / `configs/runs/portfolio-paper.yaml`).
2. В админке задайте фильтры → откройте `/export`.
3. Выберите вариант:
   - **Краткий** — метрики и сравнение экспериментов (`data-summary.json`);
   - **Подробный** — то же + список сделок (`data-trades.json`).
4. Скопируйте промпт → вставьте в ChatGPT / Claude → прикрепите соответствующий JSON.

Промпт содержит только инструкции; данные — в приложенном файле (без дублирования).

Шаблоны: `internal/export/prompts/strategy_summary.md`, `strategy_detailed.md`.

---

## Модули

### OrderExecutor — `pkg/interfaces/executor.go`

Интерфейс, абстрагирующий исполнение ордеров:

```go
type OrderExecutor interface {
    ExecuteOrder(ctx context.Context, order models.Order) error
    GetBalance(ctx context.Context) (float64, error)
}
```

Реализации:

| Структура | Файл | Режим |
|---|---|---|
| `VirtualExecutor` | `internal/bcs/virtual_executor.go` | `virtual` |
| `BCSClient` | `internal/bcs/client.go` | `real` |

### TickerWorker — `internal/engine/worker.go`

Изолированный торговый цикл для пары **эксперимент × тикер**:

- свою `CandleStrategy` (из `strategy.type`) и `RiskManager`;
- каналы свечей и тиков (котировки);
- мониторинг SL/TP на каждом тике, трейлинг +1R / +2R / непрерывный SL = MFE − 1R после +2R;
- опциональная задержка входа `session.entry_delay_minutes` и лимит `strategy.max_trades_per_ticker_per_day`;
- принудительное закрытие в `session.eod_close_time`;
- сброс дневного Circuit Breaker в `session.session_open_time`;
- запись закрытых сделок в SQLite (`experiment_id`, `pnl_r`, …);
- `Start(ctx, executor)` — бесконечный цикл в горутине.

Торговые события логируются через `pkg/logx` (см. [Что вы увидите в логе](#что-вы-увидите-в-логе)).

### BCS Client — `internal/bcs/client.go`

| Метод | Описание |
|---|---|
| `Connect(ctx)` | OAuth2 авторизация |
| `ExecuteOrder(order)` | Лимитный или рыночный ордер в API |
| `GetBalance()` | Свободные средства из портфеля |
| `SetWriteMode()` | Переключение на `trade-api-write` |
| `SetClassCode(code)` | Код класса инструмента |
| `SetCandleTimeFrame(tf)` | Таймфрейм свечей WebSocket |

### WebSocket — `internal/bcs/websocket.go`

| Метод | Описание |
|---|---|
| `SubscribeToCandles(ctx, ticker, ch)` | Подписка на один тикер |
| `SubscribeMarketDataFanOut(ctx, routes)` | Fan-Out: свечи + котировки (тики) → воркеры |

### Стратегии — `internal/strategy/`

Плагинная архитектура: каждая стратегия реализует `CandleStrategy` и регистрируется в реестре. Бот и optimizer используют один код.

| ID | Описание |
|---|---|
| `opening_range_continuation` | ORC — ORB + retest (**FROZEN**, main session) |
| `opening_range_fade` | OR Fade — fade ложного пробоя OR (**FROZEN**) |
| `momentum_filtered` | Momentum + SMA, long-only (**FROZEN**, afternoon) |
| `session_orc` | Session ORC — morning/evening слоты (**FROZEN**) |
| `momentum_breakout` | Пробой high/low за lookback (legacy research) |
| `opening_range` | ORB market-пробой (legacy) |
| `mean_reversion` | Fade от SMA (legacy) |

Как добавить новую стратегию и подключить в боте/optimizer: **[docs/strategies.md](docs/strategies.md)**.

### Риск-менеджмент — `internal/risk/manager.go`

- риск на сделку: `risk.risk_per_trade_percent` (по умолчанию **0.5%** депозита);
- Circuit Breaker при превышении `risk.max_daily_loss`;
- сброс счётчика убытков каждый день в `session.session_open_time`;
- `RegisterLoss` / `RegisterProfit` при закрытии позиции.

---

## Структура проекта

```
bcs-trading-bot/
├── cmd/
│   ├── bot/main.go                    # Точка входа, оркестрация, fan-out, HTTP-админка
│   └── optimizer/                     # Walk-forward optimizer
├── configs/
│   ├── champions/                     # FROZEN snapshot параметров
│   ├── runs/                          # Профили запуска бота
│   ├── shared/                        # Whitelist тикеров для optimizer
│   └── strategies/                    # Search space для optimizer
├── docs/
│   ├── strategy-research.md           # Champions, методология optimizer
│   ├── optimizer-modes.md             # Solo vs portfolio режимы
│   ├── system.md                      # Философия, риск, lifecycle сделки
│   ├── champion-*.md                  # Детали champions
│   ├── champion-baseline.md           # Baseline доходности для live
│   └── strategies.md                  # Архитектура и гайд по стратегиям
├── data/trades.db                     # SQLite (создаётся ботом)
├── internal/
│   ├── config/config.go
│   ├── bcs/
│   │   ├── client.go
│   │   ├── virtual_executor.go        # Paper trading (без логов сделок)
│   │   └── websocket.go
│   ├── engine/
│   │   ├── worker.go
│   │   └── session.go
│   ├── live/                          # HTTP UI/API админки (embed web/)
│   ├── api/                           # Архивы периодов, сборка экспорта для ИИ
│   ├── storage/sqlite/                # Миграции, аналитика
│   ├── risk/manager.go
│   └── strategy/                      # Реестр стратегий (см. docs/strategies.md)
├── pkg/
│   ├── interfaces/executor.go
│   ├── logx/logx.go                   # Цветные логи терминала
│   └── models/
├── go.mod
├── README.md
└── Roadmap.md
```

---

## Дорожная карта

План работ и недавно закрытое: **[Roadmap.md](Roadmap.md)**.

Принципы риска, lifecycle сделки, paper trading: **[docs/system.md](docs/system.md)**.

Champions и optimizer: **[docs/strategy-research.md](docs/strategy-research.md)**.

## Полезные ссылки

- [Документация BCS Trade API](https://trade-api.bcs.ru)
- [HTTP: Авторизация](https://trade-api.bcs.ru/http/authorization)
- [HTTP: Операции (ордера)](https://trade-api.bcs.ru/http/operations/create)
- [WebSocket: Обзор](https://trade-api.bcs.ru/websocket)

---

## Лицензия

Проект в стадии MVP. Используйте на свой страх и риск. Автор не несёт ответственности за торговые убытки.
