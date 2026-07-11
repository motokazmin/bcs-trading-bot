# Champion: MF Afternoon (Momentum Filtered)

**Статус: FROZEN** — не оптимизировать без явного запроса.

## Параметры (mf-wave2-narrow-rerun, seed 1)

Комиссия в backtest: **0,008% за leg** (тариф БКС «Трейдер»). Tickers: **MGNT + TATN** (финальный whitelist).

| | |
|---|---|
| Run ID | `mf-wave2-narrow-rerun` |
| Win rate | 49,3% |
| Expectancy | **+0.18R** (+179 ₽/сделку) |
| Profit factor | **1.34** |
| PnL | **+11 974 ₽** / ~2 года / депозит 200k |
| Walk-forward | **13/23** окон |
| Сделок | 67 |
| Seed2 (валидация) | +0.18R, 62 сделки, 14/23 WF |

*До rerun (flat 0,10 ₽/акция, 3 тикера в search): +0.38R, +19 864 ₽, 53 сделки, 14/23 WF.*

Сравнение с live: [`champion-baseline.md`](champion-baseline.md).

### Best params

```
lookback: 31
breakout_threshold: 0.0083
reward_ratio: 1.84
trend_sma_period: 20
strategy_entry_delay_minutes: 24
atr_multiplier: 2.73
trail_activation_r: 2.40
trail_discrete_step_r: 0.66
trail_stage_max: 3
max_entries_per_ticker_per_day: 1
long_only: true
volume_filter: true
volume_min_ratio: 2.49
session.entry_delay_minutes: 150
```

### Тикеры

- Рабочие: **MGNT, TATN**
- Конфиг: `configs/shared/tickers-mf-afternoon-mgnt-tatn.yaml`

### Артефакты

- `configs/champions/mf-afternoon-wave2-narrow.yaml`
- `results/afternoon/mf-wave2-narrow-rerun/best-config-20260711-221525.yaml`
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
