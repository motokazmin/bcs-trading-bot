# Champion: Opening Range Fade (OR Fade)

**Статус: FROZEN** — не оптимизировать без явного запроса.

## Параметры (wave1-conservative-rerun, seed 1)

Комиссия в backtest: **0,008% за leg** (тариф БКС «Трейдер»).

| | |
|---|---|
| Run ID | `wave1-conservative-rerun` |
| Win rate | 58,8% |
| Expectancy | **+0.33R** (+328 ₽/сделку) |
| Profit factor | **1.58** |
| PnL | **+11 136 ₽** / ~2 года / депозит 200k |
| Walk-forward | **13/23** окон |
| Сделок | 34 |
| Seed2 (валидация) | +0.61R, 29 сделок, 12/23 WF |

*До rerun (flat 0,10 ₽/акция): +0.25R, +32 932 ₽, 133 сделки, 15/23 WF.*

Сравнение с live: [`champion-baseline.md`](champion-baseline.md).

### Best params

```
orb_minutes: 25
breakout_threshold: 0.0038
fade_window_minutes: 57
fade_trade_end_minutes: 124
require_inside_range: false
atr_multiplier: 1.08
reward_ratio: 1.16
trail_activation_r: 1.99
trail_breakeven_r: 0.009
trail_stage_max: 3
max_entries_per_ticker_per_day: 3
```

### Тикеры

- Рабочие: **LKOH, CHMF, MOEX**
- Whitelist в коде: `ORFadeWhitelist` / `ORFadeBlacklist`
- Конфиг: `configs/shared/tickers-or-fade-conservative.yaml`

### Артефакты

- `configs/champions/or-fade-wave1-conservative.yaml`
- `results/or-fade/wave1-conservative-rerun/best-config-20260711-220326.yaml`
- `results/or-fade/runs-registry.json`

## Принцип

Ложный пробой утреннего OR → **fade** (вход против пробоя).

## Слот времени

~10:15–12:30 MSK (`fade_trade_end_minutes` ≈ 124 мин после open).

## Запуск

```bash
go run ./cmd/bot -config configs/champions/or-fade-wave1-conservative.yaml
go run ./cmd/bot -config configs/runs/portfolio-paper.yaml
```
