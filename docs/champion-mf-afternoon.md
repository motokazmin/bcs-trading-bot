# Champion: MF Afternoon (Momentum Filtered)

**Статус: FROZEN** — не оптимизировать без явного запроса.

## Параметры (mf-wave2-narrow, seed 1)

| | |
|---|---|
| Run ID | `mf-wave2-narrow` |
| Expectancy | **+0.38R** |
| Profit factor | **1.64** |
| PnL | **+19 864 ₽** / ~2 года / депозит 200k |
| Walk-forward | **14/23** окон |
| Сделок | 53 |
| Seed2 (валидация) | +0.41R, 50 сделок, 14/23 WF |

### Best params

```
lookback: 37
breakout_threshold: 0.0094
reward_ratio: 1.90
trend_sma_period: 32
strategy_entry_delay_minutes: 21
atr_multiplier: 2.19
trail_activation_r: 2.15
trail_discrete_step_r: 0.34
trail_stage_max: 5
max_entries_per_ticker_per_day: 1
long_only: true
volume_filter: false
session.entry_delay_minutes: 150
```

### Тикеры

- Рабочие: **MGNT, TATN**
- ROSN исключён (минус в by_ticker champion run)
- Конфиг: `configs/shared/tickers-mf-afternoon-mgnt-tatn.yaml`

### Артефакты

- `configs/champions/mf-afternoon-wave2-narrow.yaml`
- `results/afternoon/mf-wave2-narrow/best-config-20260711-160524.yaml`
- `results/afternoon/runs-registry.json`
- Search space: `configs/strategies/momentum-filtered-afternoon-wave2-narrow.yaml`

## Принцип

Momentum breakout + SMA-тренд, **long-only**, входы с **12:30** MSK.

## Слот времени

12:30–18:40 MSK.

## Запуск

```bash
go run ./cmd/bot -config configs/champions/mf-afternoon-wave2-narrow.yaml
go run ./cmd/bot -config configs/runs/portfolio-paper.yaml
```
