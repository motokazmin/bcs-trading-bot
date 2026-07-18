# Два режима optimizer

В `cmd/optimizer` два разных сценария. Их легко перепутать: оба крутят симуляцию на CSV, но отвечают на разные вопросы и используют разную модель счёта.

| | Solo | Portfolio |
|---|------|-----------|
| **Вопрос** | Какие гиперпараметры устойчивы на walk-forward? | Как FROZEN champions ведут себя вместе на одном счёте? |
| **CLI** | `optimizer run`, `optimizer backtest` | `optimizer portfolio-backtest` |
| **Вход** | search space YAML + universe тикеров | run YAML портфеля (`portfolio-paper.yaml`): FROZEN experiments + корневой `risk` |
| **Счёт** | один депозит на **одну** стратегию | один депозит на **все** experiments |
| **Поиск** | Random Search / WF; волны wide → narrow; проверка seed2 | нет — только прогон |
| **Типичный выход** | `best-config-*.yaml`, `optimizer-run-*.json` | метрики shared + разбивка по experiment |

Подробности флагов и scoring: [`cmd/optimizer/README.md`](../cmd/optimizer/README.md).  
Champions и baseline: [`strategy-research.md`](strategy-research.md), [`champion-baseline.md`](champion-baseline.md).

---

## 1. Solo — поиск гиперпараметров

**Зачем:** найти (или отвергнуть) набор параметров для **одной** стратегии на заданном whitelist и слоте сессии.

**Как работает:**

1. Загружаются M5 CSV (`data/history/`).
2. Генерируются walk-forward окна (`-window-months` / `-step-months`).
3. Random Search (`-trials`) семплирует конфиги из search space.
4. Каждый trial гоняется на всех окнах; score — медиана по окнам (Calmar / PnL, см. README).
5. Лучший конфиг пишется в `best-config-*.yaml` — **черновик**, не автодеплой.

Внутри trial — один `VirtualExecutor` и один `GlobalRisk` на эту стратегию и её тикеры. Конкуренции с другими стратегиями **нет**.

Один прогон `run` с seed=1 — ещё не champion. В solo обычно делают **волны** (детали и naming: [`strategy-research.md`](strategy-research.md) § «Волны»):

1. **Wide discovery** — широкий search space, первый Random Search (`-seed 1`).
2. **Narrow вокруг best** — новый search space (±10–15% или уже вокруг champion params), снова `run`. Имена вроде `wave2`, `wave2-narrow`. Цель — уточнить точку, а не заново «угадать» всё пространство.
3. **Seed2 (и при необходимости другие seed)** — тот же (или узкий) space, другой `-seed`. Проверка, что результат не случайность одного RNG. Seed2 не должен быть сильно хуже seed1 по `expectancy_r` / PF / WF; иначе overfit → отклонить или ещё одна волна, не snapshot.

`backtest` — одиночный прогон **уже известных** params без поиска (быстрая сверка), не замена seed2 и не замена narrow-волны.

**Команды:**

```bash
# полный поиск (wave / seed задаются RUN_ID и -seed в скриптах)
make optimizer-orc          # или optimizer-or-fade / optimizer-afternoon / …
# эквивалент:
bin/optimizer run -strategy opening_range_continuation -search-space … -tickers-config … -seed 1
bin/optimizer run … -seed 2   # seed2 на том же или narrow space

# один прогон уже известных params (без поиска)
bin/optimizer backtest -strategy … -search-space … -date-from … -date-to …
```

**Решение о champion** опирается на `expectancy_r`, PF, PnL в ₽, долю прибыльных окон **и** стабильность на seed2 — не только на walk-forward score одного seed. После go → snapshot в `configs/champions/`, затем проверка в режиме portfolio.

FROZEN champions без явного запроса **не** переоптимизировать.

### Антипаттерн: одна стратегия на одну акцию

Имеет смысл искать **принцип + слот + небольшой whitelist** (несколько тикеров с одним набором params), а не отдельный тюнинг «ORC только под SBER», «fade только под LKOH» и т.п.

Почему это антипаттерн:

1. **Overfit под бумагу.** На одной акции Random Search легко подгоняет params под её шум; seed2 и соседние тикеры обычно разваливаются (пример в истории проекта: ORC LKOH solo — seed1 ок, seed2 PF &lt; 1.3).
2. **Мало сделок.** На одном тикере за ~2 года часто десятки сделок или меньше — `expectancy_r`/PF нестабильны (жёлтый флаг вроде «&lt; 30 сделок за 2 года»).
3. **Нет переноса edge.** Matrix/solo по тикерам уже показывали: плюс на одном имени не переносится на другое без смены гипотезы (MF solo expansion 2026-07-16 и т.п.).
4. **Путаница с whitelist.** Нормальный wave3 — *убрать минусовые* из universe, где принцип уже живёт на нескольких бумагах. Это не то же самое, что завести N независимых «чемпионов» по одной акции.

Допустимо смотреть `by_ticker` и резать слабые имена из whitelist. Недопустимо (без очень сильной новой гипотезы) заводить отдельный search space / champion на каждый тикер.

---

## 2. Portfolio — проверка на едином счёте

**Зачем:** убедиться, что уже найденные FROZEN-конфиги совместимы, когда торгуют **вместе**: общий депозит, общий circuit breaker, one-position-per-ticker, лимит `max_parallel`.

**Как работает:**

1. Читается run YAML портфеля (`configs/runs/portfolio-paper.yaml`): секция `experiments` с уже зафиксированными params, тикерами и слотами сессии.
2. Риск берётся с **корневого** `risk` (единый счёт 200k, CB 2%, `max_parallel=5`).
3. Один `VirtualExecutor` + один `GlobalRisk` на весь портфель.
4. Непрерывный прогон истории на CSV: слоты (morning / main / evening) и тикеры конкурируют за капитал и «занятость» тикера.
5. В stdout — shared-метрики и разбивка PnL/сделок по `experiment` id.

Поиска гиперпараметров здесь нет: менять params в YAML ради «подкрутки» portfolio-backtest — антипаттерн. Плохой shared → менять состав слотов / гипотезу, не тюнить FROZEN без запроса.

Формат файла тот же, что у paper-запуска бота — чтобы offline-проверка и live/paper смотрели на один набор слотов. Сам `portfolio-backtest` бота не запускает.

**Команда:**

```bash
go run ./cmd/optimizer portfolio-backtest \
  -config configs/runs/portfolio-paper.yaml \
  -deposit 200000 -max-parallel 5
# опционально: -date-from / -date-to
```

Ожидаемые цифры и go/no-go: [`champion-baseline.md`](champion-baseline.md) § B / § C.

---

## Как они связаны

```
solo: wide → narrow вокруг best → seed2 OK
                 ↓
         champion snapshot
                 ↓
         portfolio-backtest  →  go/no-go vs baseline
```

- Solo отвечает: «есть ли устойчивый edge у этой стратегии в изоляции?» (в т.ч. на другом seed и после уточнения space).
- Portfolio отвечает: «не ломают ли слоты друг друга на одном депозите?»

Solo-плюс на одном seed не гарантирует seed2 и не гарантирует shared-плюс. Shared-плюс не повод снова крутить Random Search по FROZEN без явного запроса.
