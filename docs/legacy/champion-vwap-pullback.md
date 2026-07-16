# Champion candidate: VWAP Pullback Continuation

**Статус: candidate** (не FROZEN portfolio) — seed1+seed2 ok, 2026-07-16.

## Параметры (`wave1-mgnt-rosn`, seed 1)

Комиссия: **0,008% за leg**. Universe: **MGNT, ROSN** (TATN исключён после полного wave1).

| | seed1 `wave1-mgnt-rosn` | seed2 |
|---|---|---|
| Expectancy | **+0,26R** | **+0,27R** |
| Profit factor | **1,48** | **1,48** |
| Walk-forward | 15/23 | 15/23 |
| Сделок | 67 | 41 |
| PnL | +17 598 ₽ | +11 190 ₽ |

### Тикеры

- MGNT, ROSN
- `configs/shared/tickers-vwap-mgnt-rosn.yaml`

### Артефакты

- `configs/champions/vwap-pullback-mgnt-rosn.yaml`
- `results/vwap-pullback/wave1-mgnt-rosn/`
- `results/vwap-pullback/wave1-mgnt-rosn-seed2/`

## Принцип

Утренний OR задаёт направление → pullback к session VWAP по тренду (не mean reversion).

## Перед portfolio

Пересечение слотов с ORC (утро) и MF Afternoon (MGNT) — нужен priority rule на тикер.
