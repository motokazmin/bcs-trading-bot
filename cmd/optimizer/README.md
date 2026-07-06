# bcs-strategy-optimizer

Offline-сервис подбора гиперпараметров стратегии MomentumBreakout через walk-forward backtest и Random Search.

Отдельный бинарник — не меняет runtime `cmd/bot` напрямую, но переиспользует общие пакеты:
`internal/position`, `internal/trailing`, `internal/simulation`, `internal/strategy`, `internal/risk`.

## Сборка

```bash
go build -o bin/optimizer ./cmd/optimizer
```

## Подкоманды

### fetch-history — загрузка CSV с BCS API

Требует `BCS_REFRESH_TOKEN` в окружении.

```bash
optimizer fetch-history \
  -tickers SBER,ROSN,NVTK \
  -class-code TQBR \
  -timeframe M5 \
  -date-from 2024-01-01 \
  -date-to 2026-06-01 \
  -output-dir data/history
```

API: `GET .../candles-chart` (до 1000 баров за запрос, пагинация автоматическая).

### sync-history — инкрементальная догрузка (рекомендуется)

Список инструментов берётся из `config/optimizer/universe.yaml` (полный universe = 10 акций MOEX).

- **Первый запуск:** загружает `initial_history_years` (default 2) назад → сейчас
- **Повторный:** догружает только новые свечи с последней в CSV до текущего момента
- Если уже актуально — тикер пропускается

```bash
optimizer sync-history
# или
make sync-history
```

По умолчанию `-parallel-tickers 5` (переопределение: `PARALLEL_TICKERS=3 make sync-history`).

`fetch-history` оставлен для полной перезагрузки диапазона (legacy).

### Rate limit (429)

BCS API ограничивает частоту запросов к `candles-chart`. `sync-history` по умолчанию:

- **адаптивная пауза** (`-adaptive-delay`, default on): старт ~50 ms, ускоряется при успехе, замедляется при 429
- макс. пауза **3 s** (`-max-chunk-delay`)
- **retry с backoff** при 429/503 (до 6 попыток, 5s → 10s → … → 60s), учитывается заголовок `Retry-After`
- **append-checkpoint**: новые свечи дописываются в CSV после каждого чанка (без перезаписи всего файла)
- **лог прогресса** после каждого чанка
- **параллельная загрузка**: `-parallel-tickers 5` (общий rate limiter)

Фиксированная пауза:

```bash
bin/optimizer sync-history -adaptive-delay=false -chunk-delay 400ms
```

При повторном 429 увеличьте макс. паузу:

```bash
bin/optimizer sync-history -max-chunk-delay 10s -parallel-tickers 1
```

Или уменьшите глубину первичной загрузки: `-initial-years 1`

### run — walk-forward оптимизация

Тикеры и class_code — из `config/optimizer/universe.yaml` (флаг `-tickers` для override).
Даты по умолчанию — из загруженных CSV.

```bash
optimizer run
# или
make optimizer-run
```

Полный пример:

```bash
optimizer run \
  -universe config/optimizer/universe.yaml \
  -history-dir data/history \
  -search-space config/optimizer/search-space.yaml \
  -trials 200 \
  -output results/
```

## Формат CSV (`data/history/{ticker}.csv`)

```csv
timestamp,open,high,low,close,volume
2024-01-02T10:00:00+03:00,250.50,251.00,250.00,250.80,12345
```

- Разделитель: запятая
- Первая строка — заголовок (обязателен)
- `timestamp`: RFC3339 с timezone
- `volume`: целое число

## Walk-forward scoring

- **Train score** (mean по окнам) — используется `Searcher.Report` во время поиска
- **Test score** (median OOS по окнам) — финальное сравнение trials и ранжирование

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
