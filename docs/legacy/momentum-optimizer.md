# Momentum Breakout — optimizer (legacy)

> **Архив.** Линия **отклонена** (нет champion). Актуальный portfolio: [`../strategy-research.md`](../strategy-research.md).

**momentum_breakout** на полной сессии 10:00–18:40.

## Реестр

`results/momentum/runs-registry.json`

## Конфиги

| Файл | Назначение |
|---|---|
| `configs/strategies/momentum-day.yaml` | Discovery search space (весь день) |
| `configs/strategies/momentum-breakout.yaml` | Legacy wide (9 тикеров) |
| `configs/strategies/momentum-breakout-lean.yaml` | Lean (SBER/ROSN/NVTK) |
| `configs/shared/tickers-momentum.yaml` | 9 тикеров |
| `configs/shared/tickers-momentum-lean.yaml` | Lean 3 тикера |

## Запуск

```bash
make sync-history   # если нужно
make optimizer-momentum
```

Параметры через env:

```bash
# wave1 — discovery на 9 тикерах (default)
MOM_RUN_ID=wave1-wide make optimizer-momentum

# lean universe (live atr-1-lean)
MOM_RUN_ID=wave1-lean \
MOM_TICKERS=configs/shared/tickers-momentum-lean.yaml \
MOM_TRIALS=300 \
make optimizer-momentum
```

## Критерий успеха

- `expectancy_r` > 0 на whitelist тикерах
- `profitable_windows` ≥ 10/23
- сделок достаточно для статистики (сотни за 2 года на 3+ тикерах)

## Прогоны (2026-07-10)

| run_id | тикеры | exp_R | PnL | trades | WF | вывод |
|---|---|---:|---:|---:|---:|---|
| wave1-wide | 9 | −0.07 | −12k | 168 | 9/23 | минус; + только GAZP/CHMF |
| wave1-lean | SBER/ROSN/NVTK | −0.10 | −18k | 176 | 8/23 | live ≠ backtest |
| wave2-gazp-chmf | GAZP+CHMF | **+0.05** | +3.4k | 76 | 12/23 | слабый плюс, GAZP тянет вниз |
| wave2-chmf | CHMF | +0.03 | +1.9k | 61 | 10/23 | ещё слабее |

**Пока нет champion** — лучший кандидат `wave2-gazp-chmf`, но edge тонкий (+0.05R).

### Следующие шаги

```bash
# GAZP отдельно (был + на wave1-wide)
MOM_RUN_ID=wave2-gazp \
MOM_TICKERS=configs/shared/tickers-momentum-gazp.yaml \
make optimizer-momentum

# Узкий search вокруг wave2-gazp-chmf best
MOM_RUN_ID=wave3-narrow \
MOM_SEARCH_SPACE=configs/strategies/momentum-day-narrow.yaml \
MOM_TICKERS=configs/shared/tickers-momentum-gazp-chmf.yaml \
MOM_TRIALS=300 \
make optimizer-momentum
```

## Matrix baseline (9 тикеров, старый прогон)

- score 3.51, PnL **−8 434 ₽**, 129 сделок, **11/23** окон
- Лучший среди 4 стратегий matrix, но всё ещё минус → нужен whitelist + оптимизация
