# Как устроена торговая система

Риск-менеджмент, инструменты и жизненный цикл сделки.  
Запуск: [`runbook.md`](runbook.md). Портфель: [`portfolio.md`](portfolio.md). Стратегии в коде: [`strategies.md`](strategies.md).

---

## Философия

Рынок непредсказуем — контролируются убытки. Прибыль на дистанции за счёт асимметрии R:R и лимитов капитала, не за счёт «точного прогноза».

Production: дейтрейдинг акций MOEX (**TQBR**), M5, paper portfolio 5 champions на счёте 200k.

---

## Принципы

| Принцип | Как |
|---------|-----|
| R:R | `strategy.reward_ratio` (у champions ~1,2–1,8). Фактический R:R сделки = \|TP−entry\| / RDistance |
| Размер позиции | риск **0,5%** депозита на сделку |
| Circuit Breaker | дневной убыток ≥ **2%** → новые входы запрещены до следующего `session_open` |
| Изоляция | один `TickerWorker` на пару experiment×тикер |

---

## Инструменты

**Акции TQBR (production):** цена ₽/акцию, `step_price_value` по умолчанию 1,0; комиссия `0.00008` за leg; вход limit, выход SL/TP/EOD market. Сессии задаются per-experiment (утро / main / вечер).

**Фьючерсы SPBFUT:** `configs/runs/virtual-futures.yaml` — отдельный профиль, не часть portfolio champions.

---

## Жизненный цикл сделки

| Что | Когда | Источник |
|-----|--------|----------|
| Сигнал входа | закрытие M5 | `strategy.OnCandle` |
| SL / TP / трейлинг | между свечами | live: WebSocket quotes; backtest: intrabar OHLC |
| EOD | `eod_close_time` | market close |

1. Сигнал → PreTrade CB → расчёт лота → limit entry.
2. **Paper fill:** SL/TP закрываются **по уровню**, не по adverse tick за уровнем.
3. После **limit-fill** (вход ≠ close бара, типично ORC) — same-bar проверка OHLC; если стоп уже пробит — выход на том же баре. Вход по close (fade/MF) same-bar не применяет.
4. Трейлинг: `trail_activation_r`, `trail_breakeven_r`, `trail_stage_max`; стоп только в сторону прибыли.
5. EOD принудительно закрывает остаток.

Код: `internal/engine/worker.go`, `internal/position`, `internal/simulation`, `internal/trailing`, `internal/tradeaudit`.

---

## Audit валидности

На открытии и закрытии считаются флаги (`LIMIT_VS_CLOSE`, `ENTRY_PAST_STOP`, `SL_FILL_DRIFT`, `SAME_BAR_SL`, …).

- лог: `[AUDIT] severity=… codes=…`
- SQLite / export: `audit_severity`, `audit_codes`, `entry_bar_time`, `entry_bar_close`

Используйте при разборе аномального `pnl_r` в paper.

---

## Paper trading

`trading_mode: virtual` — котировки с BCS, исполнение в `VirtualExecutor`. Сделки → `data/trades.db`. Админка: `-http-listen` (дефолт `127.0.0.1:8091`). Логи: `pkg/logx` (`[OPEN]` / `[TP]` / `[SL]` / `[EOD]` / `[AUDIT]`).
