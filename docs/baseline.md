# Baseline портфеля

Эталон для сравнения paper/live с backtest на **едином счёте** 200 000 ₽.

```bash
go run ./cmd/optimizer portfolio-backtest \
  -config configs/runs/portfolio-paper.yaml \
  -date-from 2024-07-04 -date-to 2026-07-03
```

Модель: M5 CSV, комиссия 0,008%/leg, fill SL/TP по уровню, same-bar exit после ORC limit-fill. Без явного slippage.

---

## Shared account — 5 champions

| Метрика | Значение |
|---------|----------|
| Net PnL (~2 года) | **+276 672 ₽** (~138%, ~69%/год простая) |
| Сделок | **500** (~21/мес) |
| `expectancy_r` | **+0,55R** |
| Expectancy ₽/сделку | **+553 ₽** |
| Profit factor | **1,94** |
| Win rate | **56,4%** |
| Max DD | **~19 632 ₽** |
| ticker_busy skips | **4** |

### По слотам

| Слот | Сделок | Net PnL | exp_R |
|------|-------:|--------:|------:|
| Morning Session ORC | 102 | +51 013 ₽ | +0,50R |
| ORC main | 112 | +40 399 ₽ | +0,36R |
| OR Fade | 136 | +65 986 ₽ | +0,49R |
| MF Afternoon | 50 | +12 652 ₽ | +0,26R |
| Evening Session ORC | 100 | +106 621 ₽ | +1,07R |

---

## Что сравнивать в paper / live

| Метрика | Ориентир |
|---------|----------|
| `expectancy_r` | **+0,55R** |
| Net PnL / мес на 200k | ~+11,5k ₽ |
| Win rate | ~55–60% |
| Profit factor | > 1,3 (shared ~2,0) |
| Сделок / мес | ~20 |
| Circuit Breaker | 2% = −4 000 ₽/день max |

**Красные флаги:** exp_R < 0 два месяца подряд; PF < 1,0 на 30+ сделках; просадка > 10% депозита без восстановления.

После смены params champions, комиссии или модели fill — пересчитать через `portfolio-backtest`. Описание слотов: [`portfolio.md`](portfolio.md).
