# Champion: Opening Range Fade (OR Fade)

**Статус: FROZEN** — не оптимизировать без явного запроса.

## Параметры (wave1-conservative, seed 1)

| | |
|---|---|
| Run ID | `wave1-conservative` |
| Expectancy | **+0.25R** |
| Profit factor | **1.49** |
| PnL | **+32 932 ₽** / ~2 года / депозит 200k |
| Walk-forward | **15/23** окон |
| Сделок | 133 |
| Seed2 (валидация) | +0.56R, 63 сделки, 16/23 WF |

### Best params

```
orb_minutes: 16
breakout_threshold: 0.0028
fade_window_minutes: 13
fade_trade_end_minutes: 77
require_inside_range: true
atr_multiplier: 1.98
reward_ratio: 1.19
trail_activation_r: 2.36
trail_breakeven_r: 0.036
trail_stage_max: 3
max_entries_per_ticker_per_day: 3
```

### Тикеры

- Рабочие: **LKOH, CHMF, MOEX**
- Whitelist в коде: `ORFadeWhitelist` / `ORFadeBlacklist`
- Конфиг: `configs/shared/tickers-or-fade-conservative.yaml`

### Артефакты

- `configs/champions/or-fade-wave1-conservative.yaml`
- `results/or-fade/wave1-conservative/best-config-20260711-202517.yaml`
- `results/or-fade/runs-registry.json`

## Принцип

Ложный пробой утреннего OR → **fade** (вход против пробоя).

## Слот времени

~10:15–12:30 MSK (`fade_trade_end_minutes` ≈ 77 мин после open ≈ 11:17).

## Запуск

```bash
go run ./cmd/bot -config configs/champions/or-fade-wave1-conservative.yaml
go run ./cmd/bot -config configs/runs/portfolio-paper.yaml
```
