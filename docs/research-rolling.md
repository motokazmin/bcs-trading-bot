# Research rolling: новый контур рядом с champions

Поиск params на **полном universe** и **недавнем окне** (~1 год), затем promote
комплементарного слота (тикеры **вне** Main ORC whitelist) в paper.

## Зачем

Старый подход: params на длинной истории + узкий whitelist в коде/YAML.  
Новый: params ближе к сейчас, без blacklist при поиске; в paper — Main ORC
(MGNT/ROSN/TATN) + complement на остальных, без дубля тикеров.

## Артефакты

| Файл | Роль |
|------|------|
| [`configs/strategies/orc-research-rolling.yaml`](../configs/strategies/orc-research-rolling.yaml) | search space (orb / breakout / atr; risk fixed; `allowAllTickers: 1`) |
| [`configs/research/orc-main-wide.yaml`](../configs/research/orc-main-wide.yaml) | полный wide snapshot (все 10) для A/B |
| [`configs/champions/orc-complement-rolling.yaml`](../configs/champions/orc-complement-rolling.yaml) | complement в paper (`orc-complement`) |
| [`configs/runs/portfolio-paper.yaml`](../configs/runs/portfolio-paper.yaml) | paper: Main ORC + complement |
| [`scripts/run-orc-research-rolling.sh`](../scripts/run-orc-research-rolling.sh) | обёртка optimizer |
| [`configs/research/random-orc-main.yaml`](../configs/research/random-orc-main.yaml) | null-модель входа (опционально) |

## Порядок (сначала optimizer)

### 1. Поиск params (недавнее окно, полный `tickers.yaml`)

```bash
make optimizer-orc-research
# override: ORC_DATE_FROM=2025-05-08 ORC_DATE_TO=2026-05-08 ORC_TRIALS=200
```

Выход: `results/research/orc-rolling/<date>/best-config-*.yaml`  
Дефолты: train `2025-05-08 → 2026-05-08`, окна 3m / step 1m, 200 trials.

### 2. Обновить snapshots

Скопируйте блок `strategy` из `best-config` в:

- `configs/research/orc-main-wide.yaml` (полный список тикеров)
- `configs/champions/orc-complement-rolling.yaml` и слот `orc-complement` в `portfolio-paper.yaml`  
  (тикеры **без** MGNT/ROSN/TATN; `allow_all_tickers: true`)

### 3. Сравнить

```bash
go run ./cmd/optimizer portfolio-backtest \
  -config configs/champions/orc-wave2.yaml \
  -date-from 2025-05-08 -date-to 2026-05-08

go run ./cmd/optimizer portfolio-backtest \
  -config configs/champions/orc-complement-rolling.yaml \
  -date-from 2026-05-09 -date-to 2026-08-08

go run ./cmd/optimizer portfolio-backtest \
  -config configs/runs/portfolio-paper.yaml \
  -date-from 2026-05-09 -date-to 2026-08-08
```

### 4. Каденция reopt

Раз в 1–3 месяца: снова шаг 1–3.  
Менять Main ORC / complement в paper — только вручную после holdout.

## Важно

- `allow_all_tickers: true` обязателен для complement (иначе `ORCBlacklist` режет тикеры).
- В search крутим только вход (`orbMinutes`, `breakoutThreshold`, `atrMultiplier`); RR/trail фиксированы.
