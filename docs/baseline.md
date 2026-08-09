# Baseline портфеля

Эталон для сравнения paper/live с backtest на **едином счёте** 200 000 ₽.

```bash
go run ./cmd/optimizer portfolio-backtest \
  -config configs/runs/portfolio-paper.yaml \
  -date-from 2024-07-04 -date-to 2026-08-08
```

Модель: M5 CSV, комиссия 0,008%/leg, fill SL/TP по уровню, same-bar exit после ORC limit-fill. Без явного slippage.

Обновлено **2026-08-08**: MF Afternoon → `mf-afternoon-reopt-s2` (остальные champions без изменений).

---

## Shared account — 5 champions

Период **2024-07-04 → 2026-08-08** (актуальная история):

| Метрика | Значение |
|---------|----------|
| Net PnL | **+190 981 ₽** |
| Сделок | **756** |
| `expectancy_r` | **+0,35R** |
| Expectancy ₽/сделку | **+253 ₽** |
| Profit factor | **1,76** |
| Win rate | **56,3%** |
| Max DD | **~10 675 ₽** |
| ticker_busy skips | **2** |

Тот же портфель на окне **2024-07-04 → 2026-07-03** (для сравнения со старым эталоном): PnL **+167 069 ₽**, trades **638**, exp_R **+0,38**, PF **1,83**.

### По слотам (→ 2026-08-08)

| Слот | Сделок | Net PnL | exp_R |
|------|-------:|--------:|------:|
| Morning Session ORC | 179 | +37 920 ₽ | +0,34R |
| ORC main | 179 | +50 410 ₽ | +0,45R |
| OR Fade | 182 | +31 288 ₽ | +0,17R |
| MF Afternoon | 39 | +13 674 ₽ | +0,35R |
| Evening Session ORC | 177 | +57 688 ₽ | +0,43R |

---

## Что сравнивать в paper / live

| Метрика | Ориентир |
|---------|----------|
| `expectancy_r` | **~+0,35R** |
| Net PnL / мес на 200k | ориентир по shared backtest, не гарантированный live |
| Win rate | ~55–60% |
| Profit factor | > 1,3 (shared ~1,8) |
| Circuit Breaker | 2% = −4 000 ₽/день max |

**Красные флаги:** exp_R < 0 два месяца подряд; PF < 1,0 на 30+ сделках; просадка > 10% депозита без восстановления.

После смены params champions, комиссии или модели fill — пересчитать через `portfolio-backtest`. Описание слотов: [`portfolio.md`](portfolio.md).
