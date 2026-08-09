# Research: H1 trend-gate (M5→H1 EMA)

**Дата:** 2026-08-09  
**Ветка:** `feat/h1-trend-gate`  
**Вердикт:** часовой тренд-фильтр **не улучшает** champions MF / ORC по net PnL.
Не продвигать в `portfolio-paper` / `configs/champions/*` без новой гипотезы.

## Гипотеза

Сделки breakout/continuation **по направлению старшего тренда H1**
(fast/slow EMA) дают выше win rate / expectancy, чем без фильтра.

## Что реализовано (код)

| Компонент | Роль |
|-----------|------|
| `internal/strategy/trend_filter.go` | `calcEMA`, `trendDirection`, `trendGate`, `TrendProvider` (ресэмпл M5→H1) |
| Пилот | `momentum_filtered`, `opening_range_continuation` |
| Дефолт | `trend_gate_enabled: false` — baseline-поведение без изменений |
| Warmup | **skip** (`trend==""` → нет входа), чтобы окно сравнения было честным |
| Mode | int: `0=block`, `1=widen` (против тренда — расширенный стоп) |
| Search space | `configs/strategies/momentum-filtered-trend-gate.yaml`, `orc-trend-gate.yaml` |

**Данные:** отдельная загрузка H1 **не нужна** — ресэмпл из существующего
`data/history/*.csv` (M5). `make sync-history` только для догрузки свежих M5.

Fade-стратегии **намеренно не трогали** (with-trend gate логически противоречит fade).

## Протокол прогона

- Период full-period A/B: **2024-07-04 → 2026-08-08**, депозит 200 000 ₽
- Optimizer: 100 trials, seed 42, walk-forward (дефолтные окна)
- Champion params зафиксированы в `fixed`; крутились только `trendFast/Slow/Mode/AgainstMultiplier`
- Baseline: `configs/champions/mf-afternoon-reopt-s2.yaml`, `orc-wave2.yaml`
- Артефакты: `results/research/mf-trend-gate/`, `results/research/orc-trend-gate/`,
  `results/research/trend-gate-compare/`

```bash
# optimizer
bin/optimizer run -strategy momentum_filtered \
  -search-space configs/strategies/momentum-filtered-trend-gate.yaml \
  -tickers-config configs/shared/tickers-mf-afternoon-mgnt-tatn.yaml \
  -trials 100 -seed 42 -date-from 2024-07-04 -date-to 2026-08-08 \
  -output results/research/mf-trend-gate/

bin/optimizer run -strategy opening_range_continuation \
  -search-space configs/strategies/orc-trend-gate.yaml \
  -tickers-config configs/shared/tickers-orc.yaml \
  -trials 100 -seed 42 -date-from 2024-07-04 -date-to 2026-08-08 \
  -output results/research/orc-trend-gate/

# full-period A/B
bin/optimizer portfolio-backtest -config configs/champions/mf-afternoon-reopt-s2.yaml \
  -date-from 2024-07-04 -date-to 2026-08-08
bin/optimizer portfolio-backtest -config configs/champions/orc-wave2.yaml \
  -date-from 2024-07-04 -date-to 2026-08-08
# + best-config-*.yaml и *-trend-gate.yaml (fixed block 8/21)
```

## Результаты (full-period)

| Вариант | Сделки | Net PnL | exp_R | WR | PF |
|---------|-------:|--------:|------:|---:|---:|
| **MF baseline** (reopt-s2) | 40 | **+14 034** | +0.35 | 60% | 2.40 |
| MF best opt (`mode=widen` 8/23) | 38 | +9 262 | +0.25 | 63% | 2.56 |
| MF fixed `block` 8/21 | 13 | +6 760 | +0.53 | 85% | 5.57 |
| **ORC baseline** (wave2) | 181 | **+46 054** | +0.44 | 59% | 1.93 |
| ORC best opt (`mode=block` 10/52) | 107 | +28 685 | +0.43 | 58% | 2.02 |
| ORC fixed `block` 8/21 | 119 | +27 829 | +0.41 | 57% | 1.90 |

## Выводы на будущее

1. **H1 EMA-гейт не бьёт champions по net PnL.** Режет поток сделок сильнее,
   чем компенсирует качеством.
2. **Block на MF** поднимает WR/exp_R, но сделок слишком мало (13) — суммарный
   PnL хуже. Имеет смысл только как идея «жёсткого фильтра», не как замена слота.
3. **Widen** на MF выиграл walk-forward score, но на полном периоде слабее baseline —
   не доверять WF-score без full-period A/B.
4. **ORC:** и opt, и fixed режут ~35–40% сделок и ~40% PnL; exp_R почти не растёт.
5. Не повторять «ещё раз тот же H1 EMA gate на MF/ORC» без смены гипотезы
   (другой TF, другой индикатор тренда, другой скоуп тикеров/сессии).

## Что не утверждаем

- Не проверяли live-warmup (`TrendProvider.Warm` из CSV при старте бота).
- Не крутили одновременно SMA M5 (`trend_sma_period`) и H1 — SMA зафиксирован как у champion.
- Не тестировали на fade / session_orc evening/morning.
