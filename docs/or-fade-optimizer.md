# OR Fade — fade ложного пробоя OR (complement к ORC)

## Принцип

1. Строим opening range (первые N минут)
2. Фиксируем пробой за порог
3. Если цена **возвращается** в диапазон → вход **против** пробоя (fade)
4. Слот: ~10:15–12:30 (параметр `fadeTradeEndMinutes`)

ORC ловит настоящий пробой + retest. OR Fade — ложный пробой.

## Реестр

`results/or-fade/runs-registry.json`

## Запуск

```bash
make optimizer-or-fade
```

```bash
ORF_RUN_ID=wave1-seed2 ORF_SEED=2 make optimizer-or-fade
```

Тикеры: `configs/shared/tickers-orc-no-sber.yaml` (MGNT, ROSN, TATN)

## Wave1 (seed 1) — 2026-07-10

| | wave1 | wave1-seed2 |
|---|---:|---:|
| Expectancy | **+0.86R** | +0.28R |
| PF | **2.99** | 1.40 |
| PnL | +25.7k | +29.4k |
| Сделок | 30 | 106 |
| WF | **18/23** | 17/23 |

**Вывод:** edge есть на обоих seed, но wave1 завышен (мало сделок). Реалистичнее **+0.28–0.50R**, PnL ~+27–30k.

Лучший тикер: **MGNT** на обоих seed.

## Champion params (wave1, осторожно — 30 сделок)

```
orb_minutes: 11
breakout_threshold: 0.005
fade_window_minutes: 44
fade_trade_end_minutes: 138
require_inside_range: false
atr_multiplier: 0.95
reward_ratio: 1.77
```

Артефакты: `results/or-fade/wave1/`
