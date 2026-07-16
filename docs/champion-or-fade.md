# Champion: Opening Range Fade (OR Fade)

**Статус: FROZEN** — не оптимизировать без явного запроса.

## Параметры (wave3-narrow-afks, seed 1)

Комиссия в backtest: **0,008% за leg** (тариф БКС «Трейдер»).  
Discovery v2: AFKS добавлен после matrix + seed2 + portfolio wave3 ([`discovery-v2.md`](discovery-v2.md) на ветке `research/ticker-discovery`).

| | |
|---|---|
| Run ID | `wave3-narrow-afks` |
| Win rate | 59,6% |
| Expectancy | **+0,50R** (+500 ₽/сделку) |
| Profit factor | **1,99** |
| PnL | **+44 514 ₽** / ~2 года / депозит 200k |
| Walk-forward | **18/23** окон |
| Сделок | 89 |

*Предыдущий champion (LKOH/CHMF/MOEX): `wave1-conservative-rerun` — +0,33R, 34 сделки, 13/23 WF.*

Сравнение с live: [`champion-baseline.md`](champion-baseline.md).

### Best params

```
orb_minutes: 26
breakout_threshold: 0.00349
fade_window_minutes: 52
fade_trade_end_minutes: 106
require_inside_range: false
atr_multiplier: 1.23
reward_ratio: 1.27
trail_activation_r: 2.00
trail_breakeven_r: 0.018
trail_stage_max: 3
max_entries_per_ticker_per_day: 2
```

### Тикеры

- Рабочие: **LKOH, CHMF, MOEX, AFKS**
- Whitelist в коде: `ORFadeWhitelist` / `ORFadeBlacklist`
- Конфиг: `configs/shared/tickers-or-fade-conservative.yaml`

### Артефакты

- `configs/champions/or-fade-wave3-afks.yaml`
- `results/or-fade/wave3-narrow-afks/` (research branch, локально)
- Архив: `configs/champions/or-fade-wave1-conservative.yaml`
- Закрытый candidate +TATN (не в portfolio): `configs/champions/legacy/or-fade-plus-tatn.yaml` — см. [`legacy/frequency-hypotheses-2026-07-16.md`](legacy/frequency-hypotheses-2026-07-16.md)

## Принцип

Ложный пробой утреннего OR → **fade** (вход против пробоя).

## Слот времени

~10:15–11:50 MSK (`fade_trade_end_minutes` ≈ 106 мин после open).

## Запуск

```bash
go run ./cmd/bot -config configs/champions/or-fade-wave3-afks.yaml
go run ./cmd/bot -config configs/runs/portfolio-paper.yaml
```
