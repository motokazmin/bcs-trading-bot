# Portfolio

Production paper-портфель: **5 champions** на одном virtual-счёте 200 000 ₽.

Конфиг запуска: `configs/runs/portfolio-paper.yaml`.  
Снапшоты параметров: `configs/champions/*.yaml`.  
Эталон метрик: [`baseline.md`](baseline.md).  
Запуск: [`runbook.md`](runbook.md).

---

## Слоты (MSK)

| Слот | Experiment ID | Стратегия | Тикеры | Окно |
|------|---------------|-----------|--------|------|
| Утро | `session-orc-morning` | `session_orc` | ROSN, NVTK, MOEX, CHMF, TATN, SBER | 07:00–09:50 |
| Main ORC | `orc-wave2` | `opening_range_continuation` | MGNT, ROSN, TATN | ~10:00–10:30 |
| Fade | `or-fade-conservative` | `opening_range_fade` | LKOH, CHMF, MOEX, AFKS | ~10:15–12:30 |
| Afternoon | `mf-afternoon` | `momentum_filtered` | MGNT, TATN | 12:30–18:40 |
| Вечер | `session-orc-evening` | `session_orc` | NVTK, GAZP, ROSN, CHMF, MOEX, TATN, MGNT | 19:05–23:50 |

Общие правила счёта:

- депозит **200 000 ₽**, риск **0,5%** на сделку, CB **2%**/день;
- `max_parallel_trades: 5`, **одна позиция на тикер**;
- комиссия TQBR **0,008%** за leg;
- `stop_mode: atr`, `reward_ratio` у champions ≈ **1,2–1,8** (не фиксированное 1:3).

Champions **не переоптимизировать** без явного запроса.

---

## Стратегии в портфеле

### Opening Range Continuation (`opening_range_continuation` / `session_orc`)

Пробой opening range → вход **лимитом на ретесте** уровня. Session-варианты — тот же принцип в утреннем/вечернем окне.

| | Main ORC | Morning | Evening |
|---|---|---|---|
| Snapshot | `configs/champions/orc-wave2.yaml` | `session-orc-morning-wave2.yaml` | `session-orc-evening-wave2.yaml` |
| ORB | 15 мин | 14 мин | 11 мин |
| `reward_ratio` | ≈1,64 | ≈1,67 | ≈1,58 |
| Max entries/day | 1 | 1 | 2 |

### Opening Range Fade (`opening_range_fade`)

Ложный пробой OR → вход против пробоя.

| | |
|---|---|
| Snapshot | `configs/champions/or-fade-wave3-afks.yaml` |
| ORB / fade window | 26 / 52 мин, trade end 106 |
| `reward_ratio` | ≈1,27 |
| Max entries/day | 2 |

### Momentum Filtered Afternoon (`momentum_filtered`)

Momentum breakout + SMA-тренд, **long-only**, входы с 12:30 (`entry_delay_minutes: 150`).

| | |
|---|---|
| Snapshot | `configs/champions/mf-afternoon-reopt-s2.yaml` |
| Lookback / SMA | 39 / 37 |
| `reward_ratio` | ≈1,39 |
| Max entries/day | 2 |
| Notes | reopt-s2 (2026-08-08) на обновлённой истории; previous: `mf-afternoon-longonly-narrow-ws2.yaml` |

---

## Проверка портфеля

```bash
make sync-history
go run ./cmd/optimizer portfolio-backtest \
  -config configs/runs/portfolio-paper.yaml \
  -date-from 2024-07-04 -date-to 2026-07-03
```

Paper: `make bot` → админка `/trades`, `/export`. Runtime-флаги валидности сделок: `audit_severity` / `audit_codes` в БД и логах `[AUDIT]`.

---

## Optimizer (поиск params)

Solo walk-forward ищет параметры **одной** стратегии. Portfolio-backtest проверяет **уже зафиксированный** YAML на одном счёте.

| | Solo | Portfolio |
|---|---|---|
| CLI | `optimizer run` / `backtest` | `optimizer portfolio-backtest` |
| Вопрос | устойчивы ли params на WF? | как champions живут вместе? |
| Документ | [`optimizer-modes.md`](optimizer-modes.md) | этот файл + [`baseline.md`](baseline.md) |

Команды: `make optimizer-orc`, `make optimizer-or-fade`, `make optimizer-afternoon`.  
Выход: `results/<name>/` (локально, в git не коммитится).

Главная метрика решений — **`expectancy_r`** и net PnL в ₽, не только Calmar score.

---

## Связанные документы

| Документ | Содержание |
|----------|------------|
| [`baseline.md`](baseline.md) | Эталон shared-счёта для paper/live |
| [`strategies.md`](strategies.md) | Код стратегий, как добавить новую |
| [`system.md`](system.md) | Риск, lifecycle, исполнение |
| [`runbook.md`](runbook.md) | Запуск бота и optimizer |
| [`optimizer-modes.md`](optimizer-modes.md) | Solo vs portfolio |
