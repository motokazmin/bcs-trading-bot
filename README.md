# BCS Trading Bot (MVP)

Торговый робот на Go для [BCS Trade API](https://trade-api.bcs.ru). Предназначен для дейтрейдинга на 5-минутных свечах с жёстким риск-менеджментом.

Робот не пытается «угадать» рынок — он контролирует убытки:

- соотношение риск/прибыль **1:3**;
- риск на сделку **0.5%** от депозита;
- суточный предохранитель **Circuit Breaker**.

Поддерживает **несколько тикеров** (каждый в своей горутине) и два режима торговли:

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
- [Модули](#модули)
- [Структура проекта](#структура-проекта)
- [Дорожная карта](#дорожная-карта)

---

## Архитектура

```
┌──────────────────────────────────────────────────────────────────────────────┐
│                              cmd/bot/main.go                                 │
│                                                                              │
│  YAML-конфиг ──► OAuth2 ──► OrderExecutor (virtual / real)                   │
│                                    ▲                                         │
│  WebSocket Fan-Out ──┬──► TickerWorker [SBER]  ──► Strategy + RiskManager    │
│  (свечи + котировки) ├──► TickerWorker [GAZP]  ──► Strategy + RiskManager    │
│                      └──► TickerWorker [MIX]   ──► Strategy + RiskManager    │
└──────────────────────────────────────────────────────────────────────────────┘
```

Поток данных:

1. Загрузка YAML-конфига (`-config`), токен из `BCS_REFRESH_TOKEN`.
2. Авторизация через OAuth2 Keycloak → `access_token`.
3. Инициализация исполнителя (`VirtualExecutor` или `BCSClient`).
4. Для каждого тикера из конфига создаётся `TickerWorker` в отдельной горутине.
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

### 5. Виртуальная торговля — фьючерсы

```bash
go run ./cmd/bot -config configs/virtual-futures.yaml
```

### 6. Реальная торговля

```bash
# нужен refresh token со скоупом trade-api-write
go run ./cmd/bot -config configs/real-stocks.yaml
```

### 7. Локальный конфиг

Скопируйте профиль и настройте под себя (файлы `configs/local*.yaml` в `.gitignore`):

```bash
cp configs/virtual-sber.yaml configs/local.yaml
# отредактируйте configs/local.yaml
go run ./cmd/bot -config configs/local.yaml
```

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

### Старт бота (виртуальный режим)

```
2026/06/24 10:00:00 Запуск торгового робота БКС на Go...
2026/06/24 10:00:00 Конфиг: configs/virtual-multi.yaml | Режим: virtual | Тикеры: SBER, GAZP | Класс: TQBR | Свечи: M5
2026/06/24 10:00:00 Шаг 1: Авторизация через БКС OAuth...
2026/06/24 10:00:01 Access Token получен.
2026/06/24 10:00:01 Виртуальный исполнитель: баланс 200000 руб.
2026/06/24 10:00:01 [SBER] воркер запущен
2026/06/24 10:00:01 [GAZP] воркер запущен
2026/06/24 10:00:01 Шаг 2: Запущено 2 воркеров (депозит на тикер: 100000, лимит убытка: 2000, EOD: 18:40 МСК)
2026/06/24 10:00:01 Шаг 3: Торговый цикл активен. Мониторинг SL/TP и EOD включён.
2026/06/24 10:00:02 WebSocket: подписка 2 инструмент(ов) [SBER GAZP] — свечи M5 + котировки
```

После старта бот **молчит**, пока не накопится история свечей (`strategy.lookback` баров, по умолчанию 20 × M5 ≈ 100 минут) и не появится сигнал пробоя.

В 10:00 МСК (если бот работал через ночь) в логе появится сброс дневного лимита:
```
2026/06/25 10:00:00 [SBER] новый торговый день: дневной счётчик убытков сброшен
```

### Старт бота (реальный режим)

```
2026/06/24 10:00:00 Запуск торгового робота БКС на Go...
2026/06/24 10:00:00 Режим: real | Тикеры: SBER
2026/06/24 10:00:00 Шаг 1: Авторизация через БКС OAuth...
2026/06/24 10:00:01 Access Token получен.
2026/06/24 10:00:01 Реальный исполнитель: BCSClient (trade-api-write)
2026/06/24 10:00:02 Баланс счёта: 1543280.50 руб.
2026/06/24 10:00:02 [SBER] воркер запущен
2026/06/24 10:00:02 Шаг 2: Запущено 1 воркеров (депозит на тикер: 1000000, лимит убытка: 20000)
2026/06/24 10:00:02 Шаг 3: Торговый цикл активен. Ожидание сигналов...
2026/06/24 10:00:03 WebSocket: подписка на TQBR:SBER (M5)
```

### Открытие и закрытие позиции (виртуальный режим)

```
2026/06/24 11:35:00 [SBER] Сделка валидирована риском, объем 40 лотов, отправка | BUY SBER @ 250.00, SL=248.75, TP=253.75
2026/06/24 11:35:00 [VIRTUAL] ОТКРЫТИЕ BUY SBER x40 @ 250.00 | баланс=90000.00 | риск(SL)=50.00 | потенциал(TP)=150.00 | SL=248.75 TP=253.75
2026/06/24 11:42:15 [VIRTUAL] [STOP-LOSS СРАБОТАЛ] Тикер: SBER, Убыток: 50.00, Новый баланс: 89950.00
2026/06/24 11:42:15 [SBER] позиция закрыта (STOP_LOSS), PnL=-50.00
```

Расшифровка строки `[VIRTUAL] ОТКРЫТИЕ`:

| Поле | Значение | Смысл |
|---|---|---|
| `BUY SBER x40 @ 250.00` | направление, тикер, лоты, цена | Виртуальное исполнение |
| `баланс=90000.00` | остаток | Списано 40 × 250 = 10 000 руб. |
| `риск(SL)=50.00` | сумма риска | Убыток при срабатывании стопа |
| `потенциал(TP)=150.00` | потенциал | Прибыль при срабатывании тейка (1:3) |
| `SL=248.75 TP=253.75` | уровни | Расчётные стоп и тейк |

### Отклонённые сигналы

```
2026/06/24 12:00:00 [GAZP] сигнал BUY отклонён: risk manager: дневной лимит убытков превышен, торговля заблокирована
2026/06/24 12:05:00 [SBER] сигнал SELL отклонён: нулевой объём позиции
2026/06/24 12:10:00 [SBER] ошибка исполнения ордера: [VIRTUAL] недостаточно средств: нужно 500000.00, доступно 90000.00
```

### Обрыв WebSocket (автоматический реконнект)

```
2026/06/24 13:00:00 WebSocket рыночных данных: ошибка чтения: ..., переподключение через 1s
2026/06/24 13:00:02 WebSocket: подписка 1 инструмент(ов) [SBER] — свечи M5 + котировки
```

Задержка растёт экспоненциально: 1 с → 2 с → 4 с → … → 60 с.

### Остановка

```
^C
2026/06/24 18:00:00 [SBER] воркер остановлен
2026/06/24 18:00:00 [GAZP] воркер остановлен
2026/06/24 18:00:00 Завершение работы...
```

### Критические ошибки (бот завершается)

```
2026/06/24 10:00:00 КРИТИЧЕСКАЯ ОШИБКА: задайте переменную окружения BCS_REFRESH_TOKEN
2026/06/24 10:00:01 Ошибка загрузки конфига: ...
2026/06/24 10:00:01 Авторизация провалена: брокер отклонил авторизацию, статус: 401
2026/06/24 10:05:00 Стрим рыночных данных остановлен: ошибка подключения WebSocket: ...
```

---

## Конфигурация

Все настройки — в YAML-файлах каталога `configs/`. Единственный секрет — `BCS_REFRESH_TOKEN` в env.

### Готовые профили

| Файл | Назначение |
|---|---|
| `configs/virtual-sber.yaml` | Paper trading, один тикер SBER |
| `configs/virtual-multi.yaml` | Paper trading, SBER + GAZP |
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
| `virtual.balance` | = `risk.deposit` | Стартовый баланс в virtual-режиме |
| `session.timezone` | `Europe/Moscow` | Часовой пояс для EOD-логики |
| `session.eod_close_time` | `23:40` | Принудительное закрытие позиций (ЧЧ:ММ) |
| `session.session_open_time` | `10:00` | Начало торгового дня (ЧЧ:ММ); в этот момент сбрасывается дневной счётчик убытков |

При нескольких тикерах `risk.deposit` и `risk.max_daily_loss` **делятся поровну** между воркерами.

### Получение refresh token

1. Зайдите в [trade-api.bcs.ru](https://trade-api.bcs.ru).
2. Сгенерируйте refresh token в личном кабинете API.
3. Для `trading_mode: real` убедитесь, что токен имеет скоуп **trade-api-write**.
4. Сохраните токен в переменную окружения — **не коммитьте его в git**.

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

Изолированный торговый цикл для одного тикера:

- свой `MomentumBreakout` и `RiskManager`;
- каналы свечей и тиков (котировки);
- мониторинг SL/TP на каждом тике;
- принудительное закрытие в `session.eod_close_time`;
- сброс дневного Circuit Breaker в `session.session_open_time`;
- `Start(ctx, executor)` — бесконечный цикл в горутине.

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
├── cmd/bot/main.go                    # Точка входа, оркестрация
├── configs/                           # YAML-профили запуска
│   ├── virtual-sber.yaml
│   ├── virtual-multi.yaml
│   ├── virtual-futures.yaml
│   └── real-stocks.yaml
├── internal/
│   ├── config/config.go               # Загрузка и валидация конфига
│   ├── bcs/
│   │   ├── client.go                  # REST API, реальный исполнитель
│   │   ├── virtual_executor.go        # Paper trading
│   │   └── websocket.go               # WebSocket Fan-Out
│   ├── engine/
│   │   ├── worker.go                  # TickerWorker (горутина на тикер)
│   │   └── session.go                 # Торговое окно, EOD, сброс лимита
│   ├── risk/manager.go
│   └── strategy/momentum.go
├── pkg/
│   ├── interfaces/executor.go         # OrderExecutor
│   └── models/models.go
├── go.mod
└── README.md
```

---

## Дорожная карта

**Готово:**

- [x] OAuth2 авторизация
- [x] WebSocket-поток 5-минутных свечей (multi-ticker Fan-Out)
- [x] Стратегия пробоя уровней
- [x] Расчёт лота и Circuit Breaker
- [x] Paper trading (`VirtualExecutor`)
- [x] Масштабирование: воркер на тикер
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
