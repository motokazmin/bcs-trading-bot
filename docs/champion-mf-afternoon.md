# Champion: MF Afternoon (Momentum Filtered)

**Статус: FROZEN** — не оптимизировать без явного запроса.

## Параметры (mf-longonly-narrow-ws2-seed1)

Комиссия в backtest: **0,008% за leg** (тариф БКС «Трейдер»). Tickers: **MGNT + TATN**.

| | |
|---|---|
| Run ID | `mf-longonly-narrow-ws2-seed1` |
| Win rate | 51,8% |
| Expectancy | **+0.23R** (+226 ₽/сделку) |
| Profit factor | **1.59** |
| PnL | **+12 666 ₽** / ~2 года / депозит 200k |
| Walk-forward | **12/23** окон |
| Сделок | 56 |
| Seed2 (валидация) | +0.28R, PF 1.72, 55 сделок, 12/23 WF |

Сравнение с live: [`champion-baseline.md`](champion-baseline.md).

### Best params

```
lookback: 36
breakout_threshold: 0.0063
reward_ratio: 1.27
trend_sma_period: 28
strategy_entry_delay_minutes: 24
atr_multiplier: 2.85
trail_activation_r: 2.92
trail_discrete_step_r: 0.37
trail_stage_max: 1
max_entries_per_ticker_per_day: 3
long_only: true
volume_filter: true
volume_min_ratio: 1.41
session.entry_delay_minutes: 150
```

### Тикеры

- Рабочие: **MGNT, TATN**
- Конфиг: `configs/shared/tickers-mf-afternoon-mgnt-tatn.yaml`

### Артефакты

- `configs/champions/mf-afternoon-longonly-narrow-ws2.yaml`
- `results/afternoon/mf-longonly-narrow-ws2-seed1/`
- Search space: `configs/strategies/momentum-filtered-afternoon-longonly-narrow-ws2.yaml`

## Принцип

Momentum breakout + SMA-тренд, **long-only**, входы с **12:30** MSK.

## Слот времени

12:30–18:40 MSK.

## Запуск

```bash
go run ./cmd/bot -config configs/champions/mf-afternoon-longonly-narrow-ws2.yaml
go run ./cmd/bot -config configs/runs/portfolio-paper.yaml
```
