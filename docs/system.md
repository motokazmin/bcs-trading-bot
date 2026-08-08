# Как устроена торговая система

Философия, риск-менеджмент, инструменты и жизненный цикл сделки.  
**Не roadmap** — план работ: [Roadmap.md](../Roadmap.md). **Запуск бота/optimizer:** [runbook.md](runbook.md). **Champions:** [strategy-research.md](strategy-research.md). **Режимы optimizer:** [optimizer-modes.md](optimizer-modes.md).

---

## 1. Философия и цель

Большинство роботов терпят неудачу, пытаясь идеально предсказать рынок. Здесь другая парадигма: **рынок непредсказуем, убытки — под контролем.**

**Цель:** автоматизированный агент для **внутридневной торговли акциями** MOEX (класс **TQBR**, API БКС). Исследования и FROZEN portfolio — ORC, OR Fade, MF Afternoon, Morning/Evening Session ORC; депозит 200k, слоты 07:00–09:50 / 10:00–18:40 / 19:05–23:50 MSK, M5.

Прибыль на дистанции — за счёт **асимметрии рисков** и контроля капитала, а не высокой точности прогнозов.

---

## 2. Ключевые принципы

### 2.1. Асимметрия R:R

Win rate **~40–55%** (зависит от стратегии).  
`strategy.reward_ratio` в конфиге (у champions ~1,16–1,84), не фиксированное 1:3.

### 2.2. Размер позиции

Риск **0,5% депозита** на сделку по тикеру.  
Стоп далеко → меньше **акций** в лоте, убыток в ₽ не превышает лимит.

### 2.3. Circuit Breaker

Суммарный дневной убыток **≥ 2% депозита** → новые входы запрещены до **`session.session_open_time`** следующего дня. Открытая позиция сопровождается (SL/TP/EOD).

### 2.4. One ticker — one worker

Каждый тикер — отдельная горутина (`MGNT`, `LKOH`, …). Сбой по одному не блокирует остальные.

---

## 3. Инструменты

### 3.1. Акции TQBR (production)

1. **Объём и PnL:** `quantity` — число акций (лот MOEX = 1 шт. для наших тикеров; биржевой lot size не запрашивается). Цена — **₽/акцию**. Gross PnL: `(exit − entry) × quantity × step_price_value`; default `step_price_value: 1.0` → **1 ₽ на акцию = 1 ₽ PnL**.
2. **Комиссия:** `costs.commission_rate_per_leg: 0.00008` (0,008% / leg).
3. **Шорты (ORC, OR Fade):** нужна маржа на счёте; **ГО в коде не моделируется** — [Roadmap.md](../Roadmap.md).
4. **Сессия:** per-experiment окна (main 10:00–18:40; morning 07:00–09:50; evening 19:05–23:50); EOD — `session.eod_close_time` эксперимента.
5. **Ордера:** вход — **limit**; выход SL/TP/EOD — **market**.

### 3.2. Фьючерсы SPBFUT (legacy)

`configs/runs/virtual-futures.yaml` — не использовались в champion-исследованиях. EOD **23:40**, комиссия flat 5 ₽/контракт.

---

## 4. Жизненный цикл сделки

| Что | Когда | Источник |
|-----|--------|----------|
| **Сигналы** (вход) | Закрытие **M5-свечи** | `candleChan` → `strategy.OnCandle` |
| **SL/TP, трейлинг, EOD** | Между свечами | **Live:** WebSocket → `tickChan`; **Backtest:** intrabar OHLC внутри бара |

**Этапы:**

1. **Сигнал** — M5, стратегии ORC / OR Fade / MF ([strategy-research.md](strategy-research.md)). Без второй позиции на тикер. ORC/Fade/Session ORC — вход после `orb_minutes`; MF — после `entry_delay` и накопленного `lookback`.
2. **Риск** — Circuit Breaker → SL/TP (`reward_ratio`, ATR/range) → лот 0,5%.
3. **Трейлинг** — по котировкам (live) или intrabar (backtest). Параметры: `trail_activation_r`, `trail_breakeven_r`, `trail_discrete_step_r`, `trail_stage_max`. После max stage: MFE − 1R. Стоп только в сторону прибыли.
4. **Исполнение SL/TP (paper)** — выход по уровню стопа/тейка (не по adverse tick за ним). После **limit-fill** (вход ≠ close бара, ORC retest) — same-bar проверка OHLC; вход по close (fade/MF) same-bar не применяет.
5. **EOD** — принудительное закрытие в `eod_close_time`.

Код: `internal/engine/worker.go`, `internal/position` (`ExitFillPrice`, `SameBarExitAfterFill`), `internal/simulation/portfolio.go`, `internal/trailing/`.

---

## 5. Paper trading и экономика

`trading_mode: virtual` — WebSocket БКС, исполнение в `VirtualExecutor`.

**Комиссия в net PnL:** TQBR 0,008%/leg; SPBFUT flat 5 ₽ (legacy). Bot, optimizer, SQLite.

**Отладка:** закомментированные fake-сигналы в `internal/strategy/momentum.go` (быстрый прогон SL/TP/EOD).

**Логи:** `pkg/logx` — `[OPEN]` / `[TP]` / `[SL]` / `[EOD]`, PnL в ₽ и R. Персистентность: `data/trades.db`. Админка: HTTP UI/API бота (`-http-listen`, по умолчанию `127.0.0.1:8091`).

---

## Связанные документы

| Документ | Содержание |
|----------|------------|
| [strategy-research.md](strategy-research.md) | Champions, whitelist, optimizer |
| [strategies.md](strategies.md) | Код стратегий, подключение |
| [champion-baseline.md](champion-baseline.md) | Эталон метрик для live |
| [Roadmap.md](../Roadmap.md) | План работ |
| [README.md](../README.md) | Запуск, конфиг, архитектура |
