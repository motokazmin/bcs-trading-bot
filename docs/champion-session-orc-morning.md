# Champion: Morning Session ORC

**Статус: FROZEN** — не оптимизировать без явного запроса.

## Параметры (wave2-morning-orc-wl, seed 1)

Комиссия: **0,008% за leg**. Сессия: **07:00–09:50** MSK, `weekdays_only`.

| | |
|---|---|
| Strategy ID | `session_orc` |
| Run ID | `wave2-morning-orc-wl` |
| Expectancy | **+0.47R** |
| Profit factor | **1.91** |
| PnL | **+44 710 ₽** / ~2 года / 200k |
| Walk-forward | **16/23** |
| Сделок | 95 |
| Seed2 | +0.41R, PF 1.70, 116 сделок, **13/23** WF |

### Best params

```
orb_minutes: 14
atr_multiplier: 0.89
reward_ratio: 1.67
breakout_threshold: 0.0038
trail_activation_r: 1.07
trail_breakeven_r: 0.049
trail_stage_max: 3
max_entries_per_ticker_per_day: 1
```

### Тикеры

**ROSN, NVTK, MOEX, CHMF, TATN, SBER**  
(без MGNT/GAZP/LKOH)

### Артефакты

- `configs/champions/session-orc-morning-wave2.yaml`
- `configs/strategies/session-orc-morning.yaml`
- `configs/shared/tickers-session-orc-morning.yaml`
- `results/session-morning-orc/runs-registry.json`

## Принцип

Тот же ORC (пробой OR → лимит на ретесте), но на **утренней** сессии MOEX. EOD 09:50 — до main ORC 10:00.

## Запуск

```bash
# Solo
go run ./cmd/bot -config configs/champions/session-orc-morning-wave2.yaml

# Paper portfolio (5 FROZEN)
go run ./cmd/bot -config configs/runs/portfolio-paper.yaml
```

Сравнение с live: [`champion-baseline.md`](champion-baseline.md) § C.
