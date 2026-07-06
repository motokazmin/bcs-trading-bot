# bcs-strategy-optimizer

Offline-подбор гиперпараметров торговых стратегий: walk-forward backtest + Random Search.

Отдельный бинарник — не трогает `cmd/bot`, но использует **тот же код стратегий и тот же торговый цикл** (`internal/simulation` ≈ `engine.TickerWorker`).

## Содержание

- [Подход](#подход)
- [Как интерпретировать результат](#как-интерпретировать-результат)
- [Быстрый старт](#быстрый-старт)
- [Команды и флаги](#команды-и-флаги)
- [Конфигурация](#конфигурация)
- [Scoring и комиссия](#scoring-и-комиссия)
- [Производительность](#производительность)
- [Архитектура (для разработчиков)](#архитектура-для-разработчиков)

---

## Подход

### Какой вопрос мы задаём

Optimizer отвечает не на «какой максимальный PnL на всей истории», а на:

> **Если подобрать параметры на прошлом и торговать на следующем куске времени — будет ли это хоть сколько-нибудь устойчиво?**

```mermaid
flowchart TB
    subgraph question["Вопрос"]
        Q["Параметры, найденные на прошлом,<br/>работают на будущем?"]
    end

    subgraph input["Вход"]
        H["~2 года M5-истории<br/>9 акций MOEX"]
        P["Search space:<br/>lookback, ATR, trailing…"]
    end

    subgraph method["Метод"]
        WF["Walk-forward<br/>17 окон train → test"]
        RS["Random Search<br/>200 случайных наборов"]
    end

    subgraph output["Выход"]
        RANK["Ранжирование по OOS test score"]
        YAML["best-config.yaml"]
        DEPLOY{"OOS в плюсе?"}
    end

    H --> WF
    P --> RS
    WF --> RS
    RS --> RANK --> YAML
    RANK --> DEPLOY
    DEPLOY -->|да| OK["Можно рассматривать вручную"]
    DEPLOY -->|нет| WARN["WARN: не деплоить"]
```

### Walk-forward — почему не один backtest на всём периоде

Один прогон на 2024–2026 легко **переобучить**: параметры запомнят конкретные движения. Walk-forward режет историю на скользящие окна и каждый раз проверяет на **невидимом** куске (out-of-sample).

```mermaid
flowchart LR
    subgraph timeline["Временная шкала (пример)"]
        direction LR
        A1["Train<br/>6 мес"] --> B1["Test<br/>2 мес ✓ OOS"]
        B1 -.сдвиг 1 мес.-> A2["Train<br/>6 мес"]
        A2 --> B2["Test<br/>2 мес ✓ OOS"]
        B2 -.…-> DOT["× 17 окон"]
    end
```

| Часть окна | Роль |
|------------|------|
| **Train** (6 мес) | Период, в контексте которого оценивается trial |
| **Test** (2 мес) | Следующий кусок — **честный OOS**, по нему сравниваем trials |
| **Шаг** (1 мес) | Сдвиг → ~17 независимых проверок за 2 года |

**Итоговый test score trial** = **медиана** OOS по всем окнам (устойчивость важнее одного удачного квартала).

### Random Search — что ищем

Из YAML search space случайно выбирается 200 комбинаций параметров (`-trials`, `-seed`). Grid search на 10+ измерениях взорвался бы; Bayesian/TPE — возможное расширение (интерфейс `Searcher` уже есть).

**Внутри одного прогона не перебираются** (отдельные запуски CLI):

- `stop_mode` — `atr` vs `range` (`-stop-mode`)
- universe — 3 vs 9 тикеров (`-tickers`)
- тип стратегии (`-strategy` + свой search space)

### Optimizer и бот

```mermaid
flowchart LR
    subgraph offline["Optimizer — offline"]
        API["BCS API"] --> CSV["data/history/*.csv"]
        CSV --> SIM["simulation.Runner"]
        SIM --> OUT["best-config.yaml<br/>+ JSON отчёт"]
    end

    subgraph live["Бот — live"]
        WS["WebSocket M5"] --> W["TickerWorker"]
        CFG["configs/*.yaml"] --> W
        W --> DB["SQLite сделки"]
    end

    OUT -. "ручное копирование<br/>(no auto-deploy)" .-> CFG
```

| | Optimizer | Бот |
|---|-----------|-----|
| Данные | CSV-история | Live свечи |
| Цель | «Стоит ли эта гипотеза?» | «Торгуем по конфигу» |
| Параметры | Тысячи trials | Один YAML на эксперимент |
| Исполнение | `VirtualExecutor` в памяти | virtual / real |

### Scoring — как выбираем «лучший» trial

```mermaid
flowchart TD
    M[Метрики backtest] --> T{Сделок ≥ min-trades?}
    T -->|нет| INF["score = −∞<br/>trial отбракован"]
    T -->|да| P{Total PnL > 0?}
    P -->|нет| PNL["score = PnL в руб.<br/>(кто меньше теряет)"]
    P -->|да| CAL["score = Calmar<br/>PnL / MaxDrawdown"]
```

Комиссия вычитается в optimizer вручную — иначе поиск завышал бы частые сделки.

### Двухфазный режим (опционально)

Ускорение, **не** улучшение метода: фаза 1 — random search на 3 lean-тикерах; фаза 2 — top-20 пересчитываются на всех 9. Включение: `-two-phase`.

---

## Как интерпретировать результат

### Если OOS убыточен (как в текущих прогонах)

Лог: `WARN optimizer: OOS убыточен`. Это значит:

- optimizer **отработал штатно** — нашёл наименее плохой trial;
- **не** «сломался» и **не** обязательно переобучение (train тоже в минусе → edge в данных не виден);
- **не** доказательство, что прибыль невозможна в принципе — только что **в этом search space, на этом периоде и universe устойчивого плюса нет**.

| Следует | Не следует |
|---------|------------|
| Не деплоить `best-config` | Думать, что «ещё 500 trials найдут плюс» |
| Менять **гипотезу** (логику стратегии), а не только цифры | Ждать чуда от live vs backtest |
| Разобрать убыток по тикерам/окнам | Считать optimizer бесполезным |

### Если OOS в плюсе

`best-config.yaml` — **предложение**, не автодеплой. Проверить вручную: стабильность по окнам, число сделок, поведение на отдельных тикерах, затем paper → real.

---

## Быстрый старт

```bash
export BCS_REFRESH_TOKEN=...   # только для sync-history

make build-optimizer
make sync-history
make optimizer-run               # parallel = NumCPU
make strategy-matrix             # 4 стратегии, ~1–2 ч
```

Стратегии и search space:

| `-strategy` | Search space |
|-------------|--------------|
| `momentum_breakout` | `config/optimizer/search-space-momentum.yaml` |
| `momentum_filtered` | `config/optimizer/search-space-momentum-filtered.yaml` |
| `opening_range` | `config/optimizer/search-space-orb.yaml` |
| `mean_reversion` | `config/optimizer/search-space-meanrev.yaml` |

---

## Команды и флаги

### Makefile

| Target | Описание |
|--------|----------|
| `make sync-history` | Догрузка CSV |
| `make optimizer-run` | sync + оптимизация |
| `make strategy-matrix` | 4 стратегии подряд |

| Переменная | Default | Назначение |
|------------|---------|------------|
| `OPTIMIZER_PARALLEL` | `0` | Trials параллельно (`0` = все ядра) |
| `OPTIMIZER_TWO_PHASE` | — | `1` — двухфазный поиск |
| `SEARCH_SPACE` | `search-space.yaml` | YAML для `optimizer-run` |

### `optimizer run` — основные флаги

| Флаг | Default | Описание |
|------|---------|----------|
| `-strategy` | `momentum_breakout` | ID стратегии |
| `-trials` | `200` | Random search trials |
| `-train-months` / `-test-months` / `-step-months` | 6 / 2 / 1 | Walk-forward окна |
| `-min-trades` | `20` | Мин. сделок для валидного score |
| `-stop-mode` | `atr` | `atr` или `range` |
| `-parallel` | `0` | Параллельные trials |
| `-two-phase` | `false` | Lean → full universe |
| `-phase2-top` | `20` | Top-N для фазы 2 |
| `-output` | `results/` | JSON + YAML |

Полный список: `optimizer run -h`.

### Другие подкоманды

```bash
optimizer sync-history          # инкрементальная догрузка CSV
optimizer backtest ...          # один прогон без оптимизации
optimizer fetch-history ...     # полная перезагрузка (legacy)
```

---

## Конфигурация

### `config/optimizer/universe.yaml`

```yaml
lean_tickers: [SBER, ROSN, NVTK]   # для -two-phase
tickers: [SBER, GAZP, ...]         # полный universe
```

### Search space

Параметры с `min`/`max` — в random search. Секция `fixed` — риск, EOD (не ищутся).

---

## Scoring и комиссия

- Train score = **mean** по train-окнам
- Test score = **median** по test-окнам → **ранжирование trials**
- `VirtualExecutor` не вычитает комиссию из `GrossPnL`; optimizer вычитает `-commission-per-trade × quantity`
- `AggregateTrades` сортирует сделки всех тикеров по `ClosedAt` перед MaxDrawdown/Calmar

---

## Производительность

Объём: `trials × окна × 2 × тикеры` backtest'ов (~61 000 на 200 trials × 17 окон × 9 тикеров).

| Оптимизация | Что делает |
|-------------|------------|
| `PrecomputeWindowSlices` | Свечи по окнам нарезаются один раз |
| `-parallel 0` | Trials на всех ядрах CPU |
| `trialContext` | trailCfg и params — один раз на trial |

Двухфазный режим: ~3× быстрее фаза 1 (3 тикера), фаза 2 — только top-N без random search.

---

## Архитектура (для разработчиков)

```mermaid
flowchart TB
    CLI["cmd/optimizer/main.go"]

    CLI --> RUN["run"]
    CLI --> SYNC["sync-history"]
    CLI --> BT["backtest"]

    RUN --> E["Evaluator"]
    SYNC --> CSV[("data/history/*.csv")]
    CSV --> E

    E --> SL["window_slices<br/>преднарезка"]
    E --> OPT["optimization.go<br/>parallel trials"]
    OPT --> TE["trial_eval.go<br/>evaluateTrial"]

    TE --> SIM["simulation.Runner"]
    SIM --> STR["strategy"]
    SIM --> RISK["risk + trailing + position"]

    OPT --> REP["report.go<br/>JSON + best-config"]
```

### Модули `internal/optimizer/`

| Модуль | Назначение |
|--------|------------|
| `walkforward.go` | Генерация train/test окон |
| `window_slices.go` | Преднарезка свечей |
| `config.go` | Search space, `Sample()` |
| `trial_eval.go` | Один trial = все окна × тикеры |
| `objective.go` | `Score`, `Metrics`, комиссия |
| `optimization.go` | Worker pool, random search |
| `two_phase.go` | Lean → full |
| `history.go`, `fetch.go` | CSV и BCS API |
| `report.go` | Отчёты |

### Ключевые типы

```go
type TrialResult struct {
    Params     ParameterSet
    TrainScore float64   // mean по окнам
    TestScore  float64   // median OOS — ранжирование
    Windows    []WindowResult
}
```

### Design decisions

- **No auto-deploy** — `best-config` применяется вручную
- **`stop_mode` / universe** — вне search space, отдельные CLI-запуски
- **`Searcher`** — задел под TPE/Bayesian (`search.go`)

### Известные исправления (старые прогоны)

- **`rangeUseCap`** у `momentum_breakout` был инвертирован — перезапустить `-stop-mode range`
- **Equity curve** — сортировка по `ClosedAt` across тикеров (влияет на Calmar при прибыли)

---

<<<<<<< Updated upstream
Целевая метрика: **Calmar** = TotalPnL / MaxDrawdown. При `num_trades < min-trades` → score = −∞.

## Учёт комиссии

`VirtualExecutor` и `ClosedTrade.GrossPnL` **не вычитают** комиссию. Optimizer вычитает `-commission-per-trade × quantity` (round-trip за лот) при расчёте `Metrics.TotalPnL` и производных.

## Выходные файлы

- `results/optimizer-run-{timestamp}.json` — все trials + метрики по окнам
- `results/best-config-{timestamp}.yaml` — лучшая конфигурация в формате `internal/config`
- stdout: top-5 по OOS test score

## Design decision: no auto-deploy

Optimizer **только предлагает** конфиг. Ручное применение: скопировать `best-config-*.yaml` в `configs/` и запустить бота отдельно.

## Известные баги (исправлены)

**`rangeUseCap` был инвертирован для `momentum_breakout` (fixed после первого прогона).**
`momentumBreakoutOptsFromParams` считал `RangeUseCap: !paramsBoolDefault(...)` — с
отрицанием, в отличие от `mean_reversion`/`opening_range`, где та же функция
вызывается без `!`. Из-за этого при `stop_mode=range` (и как fallback у `atr`,
если ATR не считается) стоп всегда ставился без капа — `0.5 × range` вместо
`~0.5% от цены`, т.е. в разы шире, чем задумано и чем в runtime-боте
(`internal/config.StrategyOptions`/`BuildStrategy`, где `rangeUseCap` по
умолчанию `true`). Это напрямую бьёт не только по optimizer: тот же
`momentumBreakoutOptsFromParams` — единственный путь построения стратегии в
`cmd/bot` (`internal/engine/worker.go` → `StrategyConfig.BuildStrategy` →
`strategy.NewFromParams`). Баг был внесён в этом же коммите (до него —
`if s.opts.RangeUseCap { ... }` без инверсии). Сейчас все продовые конфиги
(`configs/*.yaml`) используют `stop_mode: atr`, так что live-бот на практике
не пострадал, но при переключении на `range` получил бы кратно расширенные
стопы. См. регрессионный тест
`TestMomentumBreakoutFromParamsDefaultsRangeUseCapTrue`.

Это, вероятно, основная причина катастрофического результата эксперимента
"range stop" (−430k из выжимки) — с капом в разы шире реального стопа
кардинально меняет R-дистанцию, размер позиции и достижимость тейк-профита.
**Рекомендация: перезапустить `stop_mode=range` эксперименты и
`strategy-matrix` после этого фикса** — прежние range-результаты не отражают
задуманную стратегию.

**Equity curve по портфелю считалась в неверном порядке (fixed).**
`EvaluatePeriod` прогоняла тикеры последовательно и складывала их сделки в
`netPnLs` в порядке "все сделки тикера A, затем все сделки тикера B, …", а не
в реальном хронологическом порядке закрытия позиций. `ComputeMetrics` строит
equity curve и считает `MaxDrawdown`/`Calmar` по порядку элементов среза —
т.е. просадка считалась по искусственной, не соответствующей реальности
последовательности. Пока все прогоны убыточны, `Score` использует `TotalPnL`
напрямую (сумма не зависит от порядка), поэтому ранжирование trials это не
искажало — но как только появится прибыльная конфигурация, сравнение по
`Calmar` будет опираться на бессмысленный `MaxDrawdown`. Исправлено:
`AggregateTrades` сортирует сделки по `ClosedAt` перед агрегацией по всем
тикерам. См. тест `TestAggregateTradesSortsAcrossTickersByTime`.

**`test_score > 0` не означает прибыльность конфигурации в деньгах (fixed — предупреждение добавлено).**
`TestScore` — медиана `Calmar` по всем walk-forward окнам. У части окон может
быть неплохой `Calmar` (мало сделок, маленькая просадка), при этом сумма
`TotalPnL` по всем окнам всё равно отрицательна — медиана нечувствительна к
паре сильно убыточных окон. На реальных данных (см. ниже) воспроизведено:
`mean_reversion`/`atr`, `best test_score=0.6854` (положительный!), при этом
суммарный OOS PnL по всем окнам = **−42134 руб**. Раньше `RunOptimization`
предупреждал об убыточности только при `TestScore < 0`, то есть в этом случае
предупреждения не было бы вообще — конфигурация выглядела бы прибыльной.
Добавлена дополнительная проверка суммарного `TotalPnL` по окнам с явным
предупреждением. **Вывод: при выборе конфигурации из top-N всегда проверяйте
не только `test_score`, но и `test_pnl` (сумма по окнам) в JSON/выводе.**

## Результаты первого честного прогона на реальных данных (после всех фиксов)

MOEX M5, 9 акций (`config/optimizer/universe.yaml`), 2024-07-03 → 2026-07-03,
17 walk-forward окон (6m/2m/1m). Best OOS test_score / суммарный test PnL по
всем окнам:

| strategy            | stop_mode | trials | best test_score | total test PnL, руб |
|---------------------|-----------|--------|------------------|----------------------|
| momentum_breakout    | atr       | 200    | −24658           | −234 641             |
| momentum_breakout    | range     | 200    | −64601           | −605 950             |
| momentum_filtered    | atr       | 80     | −5484            | −155 472             |
| momentum_filtered    | range     | 80     | −58356           | −1 053 068           |
| opening_range        | atr       | 80     | −23362           | −145 002             |
| opening_range        | range     | 60     | −82042           | −916 718             |
| mean_reversion       | atr       | 60     | −64.85           | −71 085              |
| mean_reversion       | atr       | 200    | +0.6854          | **−42 134** (см. предупреждение выше) |
| mean_reversion       | range     | 60     | −33135           | −310 667             |

**Ни одна конфигурация на полном universe не прибыльна OOS в деньгах.**
`mean_reversion`+`atr` стабильно ближе всех к безубытку — это ожидаемо
согласуется с фиксом `rangeUseCap` (устранил искусственно раздутые убытки
`stop_mode=range`) и с тем, что момент-стратегии на M5 голубых фишках MOEX
конкурируют с HFT/маркет-мейкерами.

Отдельно прогнан `mean_reversion`/`atr` по каждому тикеру индивидуально
(100 trials, `-min-trades 10`): только **MGNT** дал положительный суммарный
test PnL (+5315 руб за 2 года, best test_score=2.36); остальные 8 тикеров —
убыток. **Это не считается найденной альфой**: +5315 руб за 2 года при
депозите 200k — статистически незначимая величина, и выбор "лучшего из 9
тикеров постфактум" — classic multiple comparisons problem (при 9 независимых
попытках шанс случайно получить хотя бы одну "прибыльную" даже при отсутствии
реального эффекта — заметно выше 5%). Для honest вывода нужно: либо
подтвердить MGNT-эффект на out-of-sample тикерах/периоде, которые не
участвовали в отборе, либо считать общий результат отрицательным.

**Общий вывод на данный момент: устойчивой прибыльной конфигурации на MOEX
2024–2026 для реализованных стратегий (`momentum_breakout`,
`momentum_filtered`, `opening_range`, `mean_reversion`) не найдено.** Фиксы
`rangeUseCap`, порядка сделок в метриках и предупреждения score/PnL сделали
вывод куда честнее (раньше `range`-эксперименты были искусственно хуже, чем
есть на самом деле, а `atr`-эксперименты могли ложно выглядеть прибыльными
по `test_score`), но не изменили качественный результат: edge на этом
universe/периоде для данного набора стратегий не подтверждён.

## Калибровка min-trades

Порог по умолчанию `20` — стартовая гипотеза. После первого прогона проверьте распределение `num_trades` в JSON-отчёте. Если большинство trials отбраковывается — снизьте до 10–15 или увеличьте `-train-months`.

## Расширение алгоритма поиска

Интерфейс `optimizer.Searcher` готов для замены Random Search на TPE/Bayesian (см. TODO в `internal/optimizer/search.go`).
=======
Сборка: `go build -o bin/optimizer ./cmd/optimizer`
>>>>>>> Stashed changes
