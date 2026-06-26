# BCS Trading Bot (MVP)

Торговый робот на Go для [BCS Trade API](https://trade-api.bcs.ru). Предназначен для дейтрейдинга на 5-минутных свечах с жёстким риск-менеджментом.

Робот не пытается «угадать» рынок — он контролирует убытки:

- соотношение риск/прибыль **1:3**;
- риск на сделку **0.5%** от депозита;
- суточный предохранитель **Circuit Breaker**.

Поддерживает **несколько тикеров** (каждый в своей горутине), **параллельные A/B-эксперименты** (`experiments` в конфиге) и два режима торговли:

| Режим | `trading_mode` в конфиге | Поведение |
|---|---|---|
| Виртуальный (paper trading) | `virtual` | Сделки симулируются в памяти, деньги не тратятся |
| Реальный | `real` | Ордера отправляются в BCS Trade API |

> **Важно.** Для реальной торговли нужен refresh token со скоупом `trade-api-write`. Начинайте всегда с `trading_mode: virtual` в конфиге.

---

## Содержание

- [Архитектура](#архитектура)
- [Требования](#требования)
- [Запуск](#запуск)
- [Что вы увидите в логе](#что-вы-увидите-в-логе)
- [Конфигурация](#конфигурация)
- [Веб-админка и экспорт для ИИ](#веб-админка-и-экспорт-для-ии)
- [Модули](#модули)
- [Структура проекта](#структура-проекта)
- [Дорожная карта](#дорожная-карта)

---

## Архитектура

```
┌──────────────────────────────────────────────────────────────────────────────┐
│                              cmd/bot/main.go                                 │
│                                                                              │
│  YAML-конфиг ──► OAuth2 ──► OrderExecutor на эксперимент (virtual / real)   │
│                                    ▲                                         │
│  WebSocket Fan-Out ──┬──► TickerWorker [baseline/SBER] ──► Strategy + Risk  │
│  (свечи + котировки) ├──► TickerWorker [atr-2/SBER]    ──► Strategy + Risk  │
│                      └──► TickerWorker [atr-3/GAZP]    ──► Strategy + Risk  │
└──────────────────────────────────────────────────────────────────────────────┘
```

При секции `experiments` в конфиге создаётся **отдельный виртуальный счёт на каждый эксперимент**; внутри эксперимента воркеры по тикерам делят один `VirtualExecutor`. Один WebSocket раздаёт рыночные данные всем воркерам (fan-out).

Поток данных:

1. Загрузка YAML-конфига (`-config`), токен из `BCS_REFRESH_TOKEN`.
2. Авторизация через OAuth2 Keycloak → `access_token`.
3. Инициализация исполнителя: один `VirtualExecutor` (или `BCSClient`) **на эксперимент**.
4. Для каждой пары «эксперимент × тикер» создаётся `TickerWorker` в отдельной горутине.
5. WebSocket подписывается на **свечи** (сигналы стратегии) и **котировки** (тики для SL/TP).
6. Воркер на каждом тике проверяет стоп/тейк и EOD; на свече — стратегию `MomentumBreakout`.
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

### 2. Токен (единственная переменная окружения)

```bash
export BCS_REFRESH_TOKEN="ваш_refresh_token"
```

Токен **не хранится в конфиге** — только в env.

### 3. Виртуальная торговля — один тикер (по умолчанию)

```bash
go run ./cmd/bot -config configs/virtual-sber.yaml
```

### 4. Виртуальная торговля — несколько акций

```bash
go run ./cmd/bot -config configs/virtual-multi.yaml
```

### 5. Параллельные A/B-эксперименты (стоп range vs ATR)

```bash
go run ./cmd/bot -config configs/experiments-multi.yaml
```

3 эксперимента × 10 тикеров = 30 воркеров, один WebSocket. Сделки пишутся в `data/trades.db` с полем `experiment_id`.

### 6. Виртуальная торговля — фьючерсы

```bash
go run ./cmd/bot -config configs/virtual-futures.yaml
```

### 7. Реальная торговля

```bash
# нужен refresh token со скоупом trade-api-write
go run ./cmd/bot -config configs/real-stocks.yaml
```

### 8. Локальный конфиг

Скопируйте профиль и настройте под себя (файлы `configs/local*.yaml` в `.gitignore`):

```bash
cp configs/virtual-sber.yaml configs/local.yaml
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
| `[MODE]` | Режим торговли и баланс virtual-счёта на эксперимент |
| `[WS]` | WebSocket: подписка, реконнект, ошибки сервера |
| `[OPEN]` | Позиция открыта |
| `[TP]` / `[SL]` / `[EOD]` | Позиция закрыта по тейку, стопу или концу дня |
| `[TRAIL]` | Трейлинг-стоп подтянул SL (+1R / +2R) |
| `[SKIP]` | Сигнал отклонён риск-менеджером |
| `[ERR]` | Ошибка исполнения или сохранения |
| `[WARN]` | Предупреждение (не фатально) |

Метка воркера: `[SBER]` (один эксперимент) или `[atr-3/TATN]` (эксперимент + тикер).

### Старт бота (виртуальный режим, несколько экспериментов)

```
10:00:00 [SYS] Запуск торгового робота БКС на Go...
10:00:00 [SYS] Конфиг: configs/experiments-multi.yaml | Режим: virtual | Тикеры: SBER, GAZP, ... | Эксперименты: baseline (...), atr-2 (...), atr-3 (...) | Класс: TQBR | Свечи: M5
10:00:00 [SYS] Шаг 1: Авторизация через БКС OAuth...
10:00:01 [SYS] Access Token получен.
10:00:01 [SYS] Хранилище сделок: data/trades.db
10:00:01 [MODE] virtual [baseline] баланс 200000 руб.
10:00:01 [MODE] virtual [atr-2] баланс 200000 руб.
10:00:01 [MODE] virtual [atr-3] баланс 200000 руб.
10:00:01 [baseline/SBER] воркер запущен
10:00:01 [atr-2/SBER] воркер запущен
...
10:00:01 [SYS] Шаг 2: Запущено 30 воркеров (3 экспериментов × 10 тикеров, EOD: 18:40 МСК)
10:00:01 [SYS] Шаг 3: Торговый цикл активен. Мониторинг SL/TP и EOD включён.
10:00:02 [WS] подписка 10 инструмент(ов) [SBER GAZP ...] — свечи M5 + котировки
```

После старта бот **молчит**, пока не накопится история свечей (`strategy.lookback` баров, по умолчанию 20 × M5 ≈ **100 минут** с начала основной сессии) и не появится сигнал пробоя.

В `session.session_open_time` (по умолчанию 10:00 МСК) сбрасывается дневной лимит убытков:

```
10:00:00 [baseline/SBER] новый торговый день: дневной счётчик убытков сброшен
```

### Старт бота (реальный режим)

```
10:00:00 [SYS] Запуск торгового робота БКС на Go...
10:00:00 [SYS] Конфиг: configs/real-stocks.yaml | Режим: real | Тикеры: SBER | Эксперименты: default | ...
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
| `configs/virtual-sber.yaml` | Paper trading, один тикер SBER |
| `configs/virtual-multi.yaml` | Paper trading, топ-10 акций TQBR |
| `configs/experiments-multi.yaml` | A/B: baseline (range) vs ATR×2 vs ATR×3, 30 воркеров |
| `configs/virtual-futures.yaml` | Paper trading, фьючерсы SPBFUT |
| `configs/real-stocks.yaml` | Реальные ордера, акции TQBR |

Запуск: `go run ./cmd/bot -config configs/virtual-sber.yaml`

### Поля конфига

| Поле | По умолчанию | Описание |
|---|---|---|
| `trading_mode` | `virtual` | `virtual` — paper trading, `real` — реальные ордера |
| `tickers` | — | Список тикеров |
| `class_code` | `TQBR` | Класс инструмента: `TQBR` (акции), `SPBFUT` (фьючерсы) |
| `candle_timeframe` | `M5` | Таймфрейм свечей WebSocket: M1, M5, M15, ... |
| `risk.deposit` | `100000` | Депозит для расчёта лота (руб.) |
| `risk.max_daily_loss` | — | Абсолютный дневной лимит убытков (руб.) |
| `risk.max_daily_loss_percent` | `2` | % от депозита, если `max_daily_loss` не задан |
| `risk.risk_per_trade_percent` | `0.5` | Риск на сделку (% депозита) |
| `strategy.lookback` | `20` | Окно свечей стратегии |
| `strategy.stop_mode` | `range` | `range` — стоп от половины диапазона; `atr` — ATR × multiplier |
| `strategy.atr_period` | `14` | Период ATR (для `stop_mode: atr`) |
| `strategy.atr_multiplier` | `2.0` | Множитель ATR для ширины стопа |
| `strategy.range_use_cap` | `true` | Ограничить range-стоп cap 0.5% от цены входа |
| `virtual.balance` | = `risk.deposit` | Стартовый баланс virtual-счёта **на эксперимент** |
| `storage.path` | `data/trades.db` | SQLite с закрытыми сделками |
| `experiments[]` | — | Параллельные virtual-счета с разными `strategy` / `risk` (только `virtual`) |
| `experiments[].id` | — | Идентификатор (`experiment_id` в БД и в логе `id/ticker`) |
| `session.timezone` | `Europe/Moscow` | Часовой пояс для EOD-логики |
| `session.eod_close_time` | `23:40` | Принудительное закрытие позиций (ЧЧ:ММ) |
| `session.session_open_time` | `10:00` | Начало торгового дня (ЧЧ:ММ); в этот момент сбрасывается дневной счётчик убытков |

При нескольких тикерах `risk.deposit` и `risk.max_daily_loss` **делятся поровну** между воркерами одного эксперимента. При секции `experiments` корневые `risk` / `strategy` / `virtual` игнорируются — параметры задаются внутри каждого элемента `experiments[]`.

### Получение refresh token

1. Зайдите в [trade-api.bcs.ru](https://trade-api.bcs.ru).
2. Сгенерируйте refresh token в личном кабинете API.
3. Для `trading_mode: real` убедитесь, что токен имеет скоуп **trade-api-write**.
4. Сохраните токен в переменную окружения — **не коммитьте его в git**.

---

## Веб-админка и экспорт для ИИ

Отдельный процесс читает `data/trades.db` (бот может работать параллельно).

```bash
go run ./cmd/admin -db data/trades.db -listen 127.0.0.1:8090
```

Откройте http://127.0.0.1:8090

| Страница / endpoint | Назначение |
|---|---|
| `/` | Дашборд, сравнение experiment_id |
| `/trades` | Таблица сделок |
| `/export` | Выгрузка для ИИ, предпросмотр промпта |
| `GET /api/export/ai` | **Пакет для ИИ** — JSON с данными + поле `prompt` |
| `GET /api/export/prompt` | Промпт в Markdown |
| `GET /api/export/trades.csv` | Сделки в CSV |

### Как анализировать с ИИ

1. Накопите сделки ботом (`configs/experiments-multi.yaml` и т.п.).
2. В админке задайте фильтры (период, эксперимент) → скачайте **export-ai.json**.
3. Скопируйте поле `prompt` из JSON (или кнопку «Копировать промпт» на `/export`) в ChatGPT / Claude.
4. При необходимости приложите весь JSON-файл — в нём summary, разбивки и полный список сделок по экспериментам.

Шаблон промпта: `internal/admin/prompts/strategy_analysis.md` — просит ИИ сравнить эксперименты, диагностировать причины убытков и предложить следующий пакет A/B тестов в формате YAML.

---

## Модули

### OrderExecutor — `pkg/interfaces/executor.go`

Интерфейс, абстрагирующий исполнение ордеров:

```go
type OrderExecutor interface {
    ExecuteOrder(order models.Order) error
    GetBalance() (float64, error)
}
```

Реализации:

| Структура | Файл | Режим |
|---|---|---|
| `VirtualExecutor` | `internal/bcs/virtual_executor.go` | `virtual` |
| `BCSClient` | `internal/bcs/client.go` | `real` |

### TickerWorker — `internal/engine/worker.go`

Изолированный торговый цикл для пары **эксперимент × тикер**:

- свой `MomentumBreakout` и `RiskManager`;
- каналы свечей и тиков (котировки);
- мониторинг SL/TP на каждом тике, трейлинг +1R / +2R;
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

### Стратегия — `internal/strategy/momentum.go`

Пробой локальных уровней на свечах (`candle_timeframe` в конфиге, по умолчанию M5). Соотношение риск/прибыль **1:3**.

**Отладочный режим:** в `OnCandle` закомментирован блок, который генерирует искусственные BUY/SELL на каждой 3-й свече — для быстрой проверки симулятора (SL/TP, EOD, Circuit Breaker) без ожидания реального пробоя. Раскомментируйте его вместо production-логики при необходимости.

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
│   ├── bot/main.go                    # Точка входа, оркестрация, fan-out
│   └── admin/main.go                  # Веб-админка и экспорт для ИИ
├── configs/
│   ├── virtual-sber.yaml
│   ├── virtual-multi.yaml
│   ├── experiments-multi.yaml
│   ├── virtual-futures.yaml
│   └── real-stocks.yaml
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
│   ├── admin/                         # HTTP UI, export AI
│   ├── storage/sqlite/                # Миграции, аналитика
│   ├── risk/manager.go
│   └── strategy/momentum.go
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

**Готово:**

- [x] OAuth2 авторизация
- [x] WebSocket-поток 5-минутных свечей (multi-ticker Fan-Out)
- [x] Стратегия пробоя уровней
- [x] Расчёт лота и Circuit Breaker
- [x] Paper trading (`VirtualExecutor`)
- [x] Параллельные A/B-эксперименты (`experiments` + fan-out WebSocket)
- [x] Персистентность сделок (SQLite) и веб-админка
- [x] Единый формат логов virtual/real (`pkg/logx`)
- [x] Масштабирование: воркер на пару эксперимент × тикер
- [x] Отправка ордеров в BCS Trade API (режим `real`)

**Планируется:**

- [ ] Автообновление `access_token` при истечении
- [ ] Переподписка WebSocket после реконнекта с новым токеном
- [ ] Учёт реального депозита из портфеля (вместо `risk.deposit` в конфиге)
- [ ] Тесты (unit + интеграционные с mock-сервером)

**Готово (риск-менеджмент):**

- [x] Мониторинг SL/TP по потоку котировок (tick-by-tick)
- [x] Принудительное закрытие позиций в `session.eod_close_time`
- [x] `RegisterLoss` / `RegisterProfit` при закрытии позиции
- [x] Рыночные ордера для закрытия (virtual + real)
- [x] YAML-конфиги с настройками сессии
- [x] Сброс дневного Circuit Breaker в `session.session_open_time`

Подробная бизнес-логика — в [Roadmap.md](Roadmap.md).

## Полезные ссылки

- [Документация BCS Trade API](https://trade-api.bcs.ru)
- [HTTP: Авторизация](https://trade-api.bcs.ru/http/authorization)
- [HTTP: Операции (ордера)](https://trade-api.bcs.ru/http/operations/create)
- [WebSocket: Обзор](https://trade-api.bcs.ru/websocket)

---

## Лицензия

Проект в стадии MVP. Используйте на свой страх и риск. Автор не несёт ответственности за торговые убытки.
