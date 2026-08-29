# Optimizer

Offline подбор гиперпараметров акций TQBR: walk-forward + Random Search.

Режимы solo / portfolio: [`docs/optimizer-modes.md`](../../docs/optimizer-modes.md).  
Портфель: [`docs/portfolio.md`](../../docs/portfolio.md). Baseline: [`docs/baseline.md`](../../docs/baseline.md).  
Запуск рядом с ботом: [`docs/runbook.md`](../../docs/runbook.md).

FROZEN champions **не** крутить без явного запроса.

Отдельный бинарник `bin/optimizer`. Тот же цикл сделки, что в боте (`internal/simulation` ≈ live-адаптер `internal/strategies/adapter`).

---

## Подход

| Элемент | Суть |
|---------|------|
| Данные | M5 CSV (`data/history/`) |
| Walk-forward | `-window-months` / `-step-months` |
| Поиск | Random Search, `-trials`, `-seed` |
| Score trial | медиана по окнам (Calmar / PnL) |
| Выход | `optimizer-run-*.json`, `best-config-*.yaml`, `charts/` |

`best-config` **не** автодеплоится — snapshot в `configs/champions/` вручную.

**Главная метрика champion — `expectancy_r` и PnL в ₽**, не только score.  
`score > 0` не гарантирует плюс в деньгах.

`-two-phase`: фаза 1 на lean-тикерах, фаза 2 — top-N на полном universe.

---

## Быстрый старт

```bash
export BCS_REFRESH_TOKEN=...   # для sync-history
make build-optimizer
make sync-history
make optimizer-orc             # → results/orc/
```

Portfolio check:

```bash
go run ./cmd/optimizer portfolio-backtest \
  -config configs/runs/portfolio-paper.yaml
```

---

## Команды

| Команда | Назначение |
|---------|------------|
| `sync-history` | догрузка CSV |
| `run` | solo walk-forward search |
| `backtest` | один прогон известных params |
| `portfolio-backtest` | shared account по run YAML |
| `charts` | HTML по results |

Типичные флаги `run`: `-strategy`, `-search-space`, `-tickers-config`, `-history-dir`, `-trials`, `-seed`, `-parallel`, `-two-phase`, `-output`.

Полный список: `bin/optimizer -h` / `bin/optimizer run -h`.

---

## Выход

Локально (gitignore):

```
results/<name>/
  optimizer-run-*.json
  best-config-*.yaml
  charts/
```

Убыточный walk-forward (`WARN … убыточен`) — штатно: edge в этом space/universe не виден. Менять гипотезу, не только число trials.
