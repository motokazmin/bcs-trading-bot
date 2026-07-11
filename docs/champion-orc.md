# Champion: Opening Range Continuation (ORC)

**Статус: FROZEN** — утренняя стратегия зафиксирована, не оптимизируем дальше без явного запроса.

## Параметры (wave2, seed 1)

| | |
|---|---|
| Run ID | `wave2` |
| Expectancy | **+0.51R** |
| Profit factor | **1.62** |
| PnL | **+57 733 ₽** / 2 года / депозит 200k |
| Walk-forward | **14/23** окон |
| Сделок | 113 |

### Best params

```
orb_minutes: 13
atr_multiplier: 0.83
reward_ratio: 1.47
breakout_threshold: 0.0086
trail_activation_r: 2.04
trail_breakeven_r: 0.028
trail_stage_max: 2
max_entries_per_ticker_per_day: 2
```

### Тикеры

- Рабочие: **MGNT, ROSN, TATN**
- SBER: исключить (стабильно минус в backtest)

### Артефакты

- `configs/champions/orc-wave2.yaml`
- `results/orc/wave2/best-config-20260710-153944.yaml` (если есть локально)
- `results/orc/runs-registry.json`

## Принцип

Пробой утреннего диапазона → **вход лимитом на ретесте** (не market). Edge в retest, не в немедленном пробое.

## Слот времени

~10:00–10:30 MSK (после ORB 13 мин).

## Запуск

```bash
# Solo
go run ./cmd/bot -config configs/champions/orc-wave2.yaml

# В portfolio
go run ./cmd/bot -config configs/runs/portfolio-paper.yaml
```
