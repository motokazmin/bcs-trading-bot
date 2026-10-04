# Два режима optimizer

Оба крутят симуляцию на CSV, но отвечают на разные вопросы.

Запуск: [`runbook.md#optimizer`](runbook.md#optimizer) · CLI: [`cmd/optimizer/README.md`](../cmd/optimizer/README.md).

| | Solo | Portfolio |
|---|------|-----------|
| **Вопрос** | Какие params устойчивы на walk-forward? | Как champions ведут себя на одном счёте? |
| **CLI** | `optimizer run`, `optimizer backtest` | `optimizer portfolio-backtest` |
| **Вход** | search space + universe тикеров | `portfolio-paper.yaml` |
| **Счёт** | один депозит на одну стратегию | один депозит на все experiments |
| **Поиск** | Random Search / WF | нет — только прогон |
| **Выход** | `best-config-*.yaml`, JSON прогона | метрики shared + разбивка по experiment |

Baseline: [`baseline.md`](baseline.md). Старый портфель чемпионов — [`legacy/portfolio.md`](legacy/portfolio.md).

---

## Solo — поиск гиперпараметров

1. Загрузка M5 CSV (`data/history/`).
2. Walk-forward окна (`-window-months` / `-step-months`).
3. Random Search (`-trials`, `-seed`).
4. Score trial — медиана по окнам (Calmar / PnL).
5. `best-config-*.yaml` — **черновик**, не автодеплой.

`best-config` — кандидат, не решение. Решение — только зарегистрированной до прогона проверкой
(правила, пороги, отказ; формат — `docs/analysis/0008`, `0012`) с переносом на тикеры вне подбора.
Оптимизатор подбирает под выбранную историю: медиана Calmar по окнам — in-sample (0003).

```bash
make optimizer-orc
# или:
bin/optimizer run -strategy opening_range_continuation -search-space … -tickers-config … -seed 1
bin/optimizer backtest -strategy … -search-space … -date-from … -date-to …
```

Не подбирать «одна стратегия — одна акция» как основной режим: шум и overfit.

---

## Portfolio — единый счёт

Прогон уже зафиксированных experiments из run YAML на одном `VirtualExecutor` + `GlobalRisk` (CB, max parallel, one-position-per-ticker).

```bash
go run ./cmd/optimizer portfolio-backtest \
  -config configs/runs/paper-m15.yaml \
  -date-from 2024-07-04 -date-to 2026-07-03
```

Эталон: [`baseline.md`](baseline.md).
