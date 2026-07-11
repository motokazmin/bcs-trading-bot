# Champion: Opening Range Continuation (ORC)

**Статус: FROZEN** — утренняя стратегия зафиксирована, не оптимизируем дальше без явного запроса.

## Параметры (wave2-rerun, seed 1)

Комиссия в backtest: **0,008% за leg** (тариф БКС «Трейдер»).

| | |
|---|---|
| Run ID | `wave2-rerun` |
| Win rate | 55,5% |
| Expectancy | **+0.49R** (+487 ₽/сделку) |
| Profit factor | **1.61** |
| PnL | **+53 559 ₽** / 2 года / депозит 200k |
| Walk-forward | **16/23** окон |
| Сделок | 110 |
| Seed2 (валидация) | +0.54R, 107 сделок, 16/23 WF |

*До rerun (flat 0,10 ₽/акция): +0.51R, +57 733 ₽, 113 сделок, 14/23 WF.*

Сравнение с live: [`champion-baseline.md`](champion-baseline.md).

### Best params

```
orb_minutes: 15
atr_multiplier: 0.84
reward_ratio: 1.64
breakout_threshold: 0.0084
trail_activation_r: 1.52
trail_breakeven_r: 0.014
trail_stage_max: 1
max_entries_per_ticker_per_day: 1
```

### Тикеры

- Рабочие: **MGNT, ROSN, TATN**
- SBER: исключить (стабильно минус в backtest)

### Артефакты

- `configs/champions/orc-wave2.yaml`
- `results/orc/wave2-rerun/best-config-20260711-214423.yaml`
- `results/orc/runs-registry.json`

## Принцип

Пробой утреннего диапазона → **вход лимитом на ретесте** (не market). Edge в retest, не в немедленном пробое.

## Слот времени

~10:00–10:30 MSK (ORB 15 мин).

## Запуск

```bash
# Solo
go run ./cmd/bot -config configs/champions/orc-wave2.yaml

# В portfolio
go run ./cmd/bot -config configs/runs/portfolio-paper.yaml
```
