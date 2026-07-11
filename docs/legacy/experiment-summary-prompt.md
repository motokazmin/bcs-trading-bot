# Промпт: strategy-matrix (архив)

> **Архив 2026-07-06.** Актуальный статус: [`../strategy-research.md`](../strategy-research.md). Optimizer: [`../../cmd/optimizer/README.md`](../../cmd/optimizer/README.md).

Контекст для сессии по **раннему** matrix-прогону (до champions ORC / OR Fade / MF).

---

## Контекст проекта

Репозиторий: **bcs-trading-bot** — Go-бот для MOEX (BCS Trade API), M5, paper/real.

Offline **`cmd/optimizer`**: walk-forward backtest + Random Search. **Auto-deploy нет.**

## Методология (на момент прогона)

- ~2 года M5, 9 акций MOEX
- Walk-forward 2/1 мес; Random Search 200 trials
- Scoring: median Calmar / PnL; комиссия 0,008%/leg

## Результаты strategy-matrix (2026-07-06)

| Стратегия | score (median) | Суммарный PnL | Окон в плюсе |
|-----------|----------------|---------------|--------------|
| mean_reversion | +0.017 | **−70 056 ₽** | 5/17 |
| opening_range | −14 763 | −48 026 ₽ | 0/17 |
| momentum_filtered | −10 126 | −80 521 ₽ | 1/17 |
| momentum_breakout | −14 875 | −91 135 ₽ | 2/17 |

Артефакты: `results/strategy-matrix-summary.json`.

## Вывод (исторический)

На полном universe 9 акций устойчивого edge не было. Champions появились после смены гипотез, whitelist и слотов времени — см. `strategy-research.md`.
