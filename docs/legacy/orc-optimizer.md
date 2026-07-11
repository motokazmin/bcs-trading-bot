# ORC — optimizer (legacy)

> **Архив.** Champion FROZEN: [`../champion-orc.md`](../champion-orc.md), [`../strategy-research.md`](../strategy-research.md).

Бот не в приоритете. Все прогоны — walk-forward backtest в `cmd/optimizer`.

## Реестр прогонов

**`results/orc/runs-registry.json`** — сравнение всех запусков ORC:
- `walk_forward`: score, прибыльные окна
- `backtest_best_config`: **expectancy_r**, PF, win rate, PnL
- `best_params`, `by_ticker`, пути к артефактам

После каждого прогона таблица сравнения печатается в консоль.

## Конфиги search space

| Файл | Назначение |
|---|---|
| `configs/strategies/orc-wave1-wide.yaml` | Широкий search (wave1, архив) |
| `configs/strategies/orc.yaml` | **Текущий default** — узкий search вокруг wave1 best |
| `configs/strategies/orc-wave2.yaml` | То же, что `orc.yaml` (явный wave2) |

Тикеры:
- `configs/shared/tickers-orc.yaml` — полный whitelist (SBER, MGNT, ROSN, TATN)
- `configs/shared/tickers-orc-no-sber.yaml` — **текущий фокус** (MGNT, ROSN, TATN)

## Запуск

```bash
make sync-history     # при необходимости
make optimizer-orc    # wave2, 300 trials → results/orc/wave2/
```

Параметры через env:

```bash
ORC_RUN_ID=wave3-no-sber \
ORC_TICKERS=configs/shared/tickers-orc-no-sber.yaml \
ORC_TRIALS=300 \
ORC_SEED=1 \
make optimizer-orc
```

Повтор широкого wave1:

```bash
ORC_RUN_ID=wave1-rerun \
ORC_SEARCH_SPACE=configs/strategies/orc-wave1-wide.yaml \
ORC_TRIALS=200 \
./scripts/run-orc-optimizer.sh
```

Только записать в реестр (без optimizer):

```bash
python3 scripts/record-orc-run.py \
  --run-id wave1-wide \
  --output-dir results/focus/exp-opening_range_continuation \
  --search-space configs/strategies/orc-wave1-wide.yaml \
  --trials 200 --seed 1
```

## Wave1 baseline (зафиксировано)

- Expectancy: **+0.46R**, PF **1.59**, 106 сделок, **12/23** окон
- Best: `orb=16`, `atr≈1.0`, `RR≈1.65`, `breakout≈0.9%`
- Артефакты: `results/focus/exp-opening_range_continuation/`

## Критерий «лучше wave1»

Одновременно:
- `expectancy_r` ≥ 0.46 (или не ниже при меньшем DD)
- `profitable_windows` ≥ 12/23
- параметры в разумном коридоре (не экстремальный выброс)

## Export

`export/data-trades.json` — поле `strategy_params` на каждой сделке.
