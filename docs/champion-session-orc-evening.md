# Champion: Evening Session ORC

**Статус: FROZEN** — не оптимизировать без явного запроса.

## Параметры (wave2-evening-orc-wl, seed 1)

Комиссия: **0,008% за leg**. Сессия: **19:05–23:50** MSK, `weekdays_only`.

| | |
|---|---|
| Strategy ID | `session_orc` |
| Run ID | `wave2-evening-orc-wl` |
| Expectancy | **+0.90R** |
| Profit factor | **2.53** |
| PnL | **+74 244 ₽** / ~2 года / 200k |
| Walk-forward | **16/23** |
| Сделок | 83 |
| Seed2 | +1.27R, PF 4.42, 33 сделки, **9/23** WF (жёлтый флаг) |

### Best params

```
orb_minutes: 11
atr_multiplier: 1.21
reward_ratio: 1.58
breakout_threshold: 0.0055
trail_activation_r: 2.05
trail_stage_max: 1
max_entries_per_ticker_per_day: 2
```

### Тикеры

**NVTK, GAZP, ROSN, CHMF, MOEX, TATN, MGNT**

### Артефакты

- `configs/champions/session-orc-evening-wave2.yaml`
- `configs/strategies/session-orc-evening.yaml`
- `configs/shared/tickers-session-orc-evening.yaml`
- `results/session-evening-orc/runs-registry.json`

## Принцип

ORC на **вечерней** сессии MOEX (после main EOD 18:40). Не путать с отклонённым `late_session_imbalance` (18:00–18:35).

## Запуск

```bash
# Solo
go run ./cmd/bot -config configs/champions/session-orc-evening-wave2.yaml

# Paper portfolio (5 FROZEN)
go run ./cmd/bot -config configs/runs/portfolio-paper.yaml
```

Сравнение с live: [`champion-baseline.md`](champion-baseline.md) § C.
