# bcs-strategy-optimizer

Offline-подбор гиперпараметров **акций TQBR**: walk-forward backtest + Random Search.

> **Статус (2026-07-11):** portfolio **FROZEN** — ORC, OR Fade, MF Afternoon.  
> Optimizer по champions — **только по явному запросу**.  
> Методология и результаты: [`docs/strategy-research.md`](../../docs/strategy-research.md) · baseline для live: [`docs/champion-baseline.md`](../../docs/champion-baseline.md).

Отдельный бинарник (`bin/optimizer`). Тот же код стратегий и тот же цикл сделки, что в боте: `internal/simulation.PortfolioRunner` ≈ `engine.TickerWorker`.

## Содержание

- [Подход](#подход)
- [Как читать результат](#как-читать-результат)
- [Быстрый старт](#быстрый-старт)
- [Команды и флаги](#команды-и-флаги)
- [Scoring и комиссия](#scoring-и-комиссия)
- [Архитектура](#архитектура)
- [Как добавить стратегию](../../docs/strategies.md)

---

## Подход

Optimizer отвечает не на «максимальный PnL на всей истории», а на:

> **Параметры, найденные на прошлых окнах, хоть сколько-нибудь устойчивы на следующих?**

| Элемент | Суть |
|---------|------|
| **Данные** | ~2 года M5 CSV, акции MOEX (`data/history/`) |
| **Walk-forward** | Окна по `-window-months` / `-step-months` (default 2 / 1 мес) |
| **Поиск** | Random Search, `-trials` (default 200), `-seed` |
| **Score trial** | **Медиана** score по окнам → ранжирование |
| **Выход** | `optimizer-run-*.json`, `best-config-*.yaml`, `charts/`, `export/` |

**Вне одного прогона** (отдельные CLI-запуски): `-stop-mode`, universe (`-tickers-config` / `-tickers`), `-strategy` + свой search space.

**Optimizer vs бот:** CSV + тысячи trials vs WebSocket + один YAML. `best-config` **не автодеплоится** — snapshot в `configs/champions/` вручную.

### Scoring (кратко)

1. Сделок < `-min-trades` → trial отбракован  
2. Total PnL ≤ 0 → score = PnL (руб.)  
3. Total PnL > 0 → score = Calmar  

**Главная метрика для решений о champion — `expectancy_r` и суммарный PnL в ₽**, не только score (см. [`strategy-research.md`](../../docs/strategy-research.md)).  
`score > 0` **не гарантирует** плюс в деньгах — всегда сверяйте с суммой PnL по окнам в JSON.

### Двухфазный режим

`-two-phase`: фаза 1 на `lean_tickers` (3 акции), фаза 2 — top-N на полном universe. Ускорение, не другой метод.

---

## Как читать результат

### Убыточный walk-forward

Лог `WARN optimizer: walk-forward убыточен` — штатно: в этом search space / universe edge не виден. Не деплоить `best-config`; менять **гипотезу** (логику, слот, whitelist), а не только число trials.

Так было на раннем **strategy-matrix** (momentum / mean_reversion на 9 тикерах). Champions нашлись после смены стратегий и whitelist — см. [`strategy-research.md`](../../docs/strategy-research.md).

### Прибыльный walk-forward

`best-config.yaml` — **черновик**. Проверить: `expectancy_r`, PF, profitable windows, by_ticker, seed2. Затем snapshot → paper → (осторожно) real.

Текущие FROZEN champions (commission-rerun, seed 1): ORC +0,49R, OR Fade +0,33R, MF +0,18R — [`champion-baseline.md`](../../docs/champion-baseline.md).

---

## Быстрый старт

```bash
export BCS_REFRESH_TOKEN=...   # только для sync-history

make build-optimizer
make sync-history

# FROZEN — только по явному запросу:
# make optimizer-orc
# make optimizer-or-fade
# make optimizer-afternoon

make optimizer-run          # sync + ORC (scripts/run-orc-optimizer.sh по умолчанию)
make charts-all             # HTML по OPTIMIZER_OUT
```

Champion snapshots: `configs/champions/*.yaml` · Paper: `configs/runs/portfolio-paper.yaml`

| `-strategy` | Search space (типичный) | Статус |
|-------------|-------------------------|--------|
| `opening_range_continuation` | `configs/strategies/orc*.yaml` | **FROZEN** |
| `opening_range_fade` | `configs/strategies/or-fade*.yaml` | **FROZEN** |
| `momentum_filtered` | `configs/strategies/momentum-filtered-afternoon*.yaml` | **FROZEN** |
| `momentum_breakout`, `opening_range`, `mean_reversion` | `configs/strategies/…` | legacy |

---

## Команды и флаги

### Makefile

| Target | Описание |
|--------|----------|
| `make sync-history` | Догрузка CSV (`TICKERS_CONFIG`, default ORC whitelist) |
| `make optimizer-run` | sync + `optimizer run` (ORC defaults) |
| `make optimizer-orc` | `scripts/run-orc-optimizer.sh` → `results/orc/<run_id>/` |
| `make optimizer-or-fade` | → `results/or-fade/` |
| `make optimizer-afternoon` | → `results/afternoon/` |
| `make optimizer-momentum` | legacy momentum |
| `make strategy-matrix` | legacy: 4 стратегии подряд |
| `make charts-all` | `optimizer charts -all` |

Скрипты пишут в `results/*/runs-registry.json` через `scripts/record-*-run.py`.  
Переменные прогона: `ORC_RUN_ID`, `ORF_RUN_ID`, `AFT_RUN_ID`, `ORC_TRIALS`, `ORC_SEED`, … — см. [`strategy-research.md`](../../docs/strategy-research.md).

| Переменная | Default | Назначение |
|------------|---------|------------|
| `TICKERS_CONFIG` | `tickers-orc-no-sber.yaml` | Universe для sync / run |
| `SEARCH_SPACE` | `configs/strategies/orc.yaml` | Search space |
| `OPTIMIZER_STRATEGY` | `opening_range_continuation` | `-strategy` |
| `OPTIMIZER_OUT` | `results/orc/` | `-output` |
| `OPTIMIZER_PARALLEL` | `0` | `0` = все ядра |
| `OPTIMIZER_TWO_PHASE` | — | `1` → `-two-phase` |

### `optimizer run` — основные флаги

| Флаг | Default | Описание |
|------|---------|----------|
| `-strategy` | `momentum_breakout`¹ | ID стратегии |
| `-search-space` | из стратегии | YAML параметров |
| `-tickers-config` | `configs/shared/tickers.yaml` | Universe + costs |
| `-trials` | `200` | Random search |
| `-seed` | now | Воспроизводимость |
| `-window-months` / `-step-months` | 2 / 1 | Walk-forward |
| `-min-trades` | `20` | Мин. сделок (scripts часто `10`) |
| `-stop-mode` | `atr` | `atr` или `range` |
| `-deposit` | `200000` | Депозит risk manager |
| `-commission-rate` | из YAML | 0,00008 = 0,008%/leg |
| `-commission-per-lot` | из YAML | flat, legacy / фьючерсы |
| `-parallel` | `0` | Параллельные trials |
| `-two-phase` | `false` | Lean → full |
| `-output` | `results/` | JSON, YAML, charts, export |

¹ CLI default legacy; для champions используйте `make optimizer-orc` или `-strategy opening_range_continuation`.

После `run` автоматически: `{output}/charts/*.html`, `{output}/export/data-*.json`.

### Другие подкоманды

```bash
optimizer sync-history [-tickers-config ...]
optimizer backtest ...
optimizer portfolio-backtest -config configs/runs/portfolio-paper.yaml \
  -date-from 2024-07-04 -date-to 2026-07-03
  # единый счёт 200k: все experiments, общий CB, one-position-per-ticker
optimizer charts -experiment orc-wave2   # или -all -results-dir results/orc/wave2-rerun
optimizer fetch-history ...              # legacy: полная перезагрузка
optimizer run -h
```

---

## Scoring и комиссия

- **Score** — median по окнам (ранжирование trials внутри прогона)
- **Net PnL** — `internal/costs`: TQBR `commission_rate_per_leg: 0.00008`; SPBFUT flat `commission_per_lot: 5.0`
- **Fill** — close свечи, intrabar OHLC для SL/TP в backtest; без slippage ([Roadmap.md](../../Roadmap.md), [system.md](../../docs/system.md))
- **Calmar** — `AggregateTrades` сортирует сделки всех тикеров по `ClosedAt`

Калибровка `-min-trades`: default 20; если большинство trials отбраковывается — 10–15 или больше `-window-months`.

---

## Архитектура

```
cmd/optimizer/main.go
  ├── run      → eval.Evaluator → core/* (search, windows, score)
  │              → report/* (JSON, best-config)
  │              → simulation.PortfolioRunner
  ├── sync-history / fetch-history → data/*, BCS API → CSV
  └── charts   → charts/* (HTML + export)
```

| Пакет | Роль |
|-------|------|
| `internal/optimizer/core` | Search space, random search, metrics, WF-окна |
| `internal/optimizer/eval` | Trials, parallel run, backtest |
| `internal/optimizer/report` | JSON, best-config, top-N |
| `internal/optimizer/charts` | Графики и export для ИИ |
| `internal/optimizer/data` | tickers.yaml, sync |

**Design:** no auto-deploy · `Searcher` — задел под TPE/Bayesian (`core/search.go`).

### Выходные файлы (типичный прогон)

```
results/orc/wave2-rerun/
  optimizer-run-20260711-*.json
  best-config-20260711-*.yaml
  charts/
  export/
```

### Исправления, влияющие на старые прогоны

| Fix | Суть |
|-----|------|
| `rangeUseCap` | Был инвертирован у `momentum_breakout` — range-прогоны до фикса недоверны |
| Equity order | Сделки сортируются по `ClosedAt` перед Calmar |
| score vs PnL | WARN, если score > 0, но сумма PnL по окнам < 0 |

Ранний strategy-matrix (2024–2026, 9 тикеров, momentum/MR) — все минус OOS; это **до** ORC/OR Fade/MF. Архив: `results/strategy-matrix-summary.json`, [`docs/legacy/`](../../docs/legacy/README.md).

---

Сборка: `go build -o bin/optimizer ./cmd/optimizer`
