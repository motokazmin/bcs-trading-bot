# Задача: реализовать сервис bcs-strategy-optimizer в cmd/optimizer

## Контекст

Репозиторий: github.com/motokazmin/bcs-trading-bot
Существующая структура:
- cmd/bot/main.go — реалтайм-вход, оркестрация воркеров через engine.NewTickerWorker,
  graceful shutdown через signal.NotifyContext
- cmd/admin/main.go — веб-админка/экспорт (SQLite + HTTP)
- internal/strategy/momentum.go — MomentumBreakout (lookback, breakoutThreshold,
  volume filter через passesVolumeFilter — активно используется)
- internal/risk/manager.go — риск-менеджмент (позиционирование, circuit breaker)
- internal/engine/worker.go — TickerWorker, closePosition, трейлинг-стоп (Variant C)
- internal/bcs/virtual_executor.go — VirtualExecutor (paper trading), реализует
  pkg/interfaces.OrderExecutor
- internal/config — YAML-конфиги эксперименты/тикеры/риск

Нужно добавить новое CLI-приложение в cmd/optimizer/main.go — offline-сервис
подбора гиперпараметров стратегии через walk-forward backtest. Это НЕ
изменение реалтайм-бота — новый независимый бинарник, использующий
существующие internal-пакеты как библиотеку (VirtualExecutor, MomentumBreakout,
risk.Manager), но не трогающий cmd/bot и его runtime-путь.

## Требования к структуре

```
cmd/optimizer/
  main.go                 — точка входа, парсинг CLI-флагов
internal/optimizer/
  config.go                — search space: границы и типы параметров
  search.go                — алгоритм поиска (см. ниже)
  walkforward.go            — генерация train/test окон по датам
  objective.go              — целевая функция (запуск backtest + метрика)
  report.go                 — экспорт результатов (JSON + человекочитаемый summary)
```

## Функциональность

### 1. Источник данных

Читает историю котировок из локального хранилища в формате **CSV**
(`data/history/{ticker}.csv`). Загрузка истории с BCS — отдельная подкоманда
`optimizer fetch-history`, результат сохраняется в тот же CSV-формат.

Схема колонок: `timestamp, open, high, low, close, volume` — точный формат
(разделитель, формат timestamp, наличие заголовка) задокументировать в
README `cmd/optimizer`.

Здесь предполагаем, что история уже загружена.

### 2. Search space

Конфигурируемый через YAML (`config/optimizer/search-space.yaml`), пример:

```yaml
parameters:
  lookback:
    type: int
    min: 10
    max: 40
  breakoutThreshold:
    type: float
    min: 0.0
    max: 0.5
  volumeFilterMultiplier:
    type: float
    min: 1.0
    max: 3.0
  atrMultiplier:
    type: float
    min: 1.0
    max: 3.0
  trailActivationR:
    type: float
    min: 1.0
    max: 3.0
  trailDiscreteStepR:
    type: float
    min: 0.25
    max: 1.0
  trailStageMax:
    type: int
    min: 1
    max: 5
  maxEntriesPerTickerPerDay:
    type: int
    min: 1
    max: 5

fixed:                    # риск-константы, НЕ участвуют в поиске
  riskPerTradePercent: 0.5
  dailyLossLimitPercent: 2.0
  eodForceCloseMinutesBeforeClose: 10
```

### 3. Алгоритм поиска

Реализовать Random Search как baseline (простая, без зависимостей),
с возможностью расширения интерфейсом `Searcher`:

```go
type Searcher interface {
    Suggest() ParameterSet
    Report(params ParameterSet, score float64)
    Best() (ParameterSet, float64)
}
```

Baseline: `RandomSearcher` (N случайных точек из search space).
Оставить TODO/интерфейс для последующей замены на TPE-подобный
алгоритм, но не реализовывать байесовскую оптимизацию в этой итерации.

Количество итераций — флаг `-trials` (default 200).

### 4. Walk-forward валидация

Обязательно, не опционально. Разбить исторический период на N окон:

```go
type Window struct {
    TrainStart, TrainEnd time.Time
    TestStart, TestEnd   time.Time
}

func GenerateWindows(fullStart, fullEnd time.Time, trainMonths, testMonths, stepMonths int) []Window
```

Оптимизация (Suggest/Report) идёт по метрике на `TrainStart..TrainEnd`.
Финальная метрика конфигурации — среднее (или медиана) по метрикам
на `TestStart..TestEnd` всех окон (out-of-sample), это и есть то, что
сравнивается между trials.

Флаги: `-train-months` (default 6), `-test-months` (default 2), `-step-months` (default 1).

### 5. Objective / метрика

Прогон `VirtualExecutor` на заданном окне с заданным `ParameterSet`.
НЕ использовать чистый PnL как метрику. Реализовать:

```go
type Metrics struct {
    TotalPnL      float64
    Sharpe        float64  // по доходностям сделок
    MaxDrawdown   float64
    Calmar        float64  // TotalPnL / MaxDrawdown
    NumTrades     int
    WinRate       float64
}

func Score(m Metrics) float64 {
    if m.NumTrades < minTradesThreshold {
        return -math.Inf(1) // отбраковка недостаточной выборки
    }
    return m.Calmar
}
```

`minTradesThreshold` — флаг, default 20.

**ВАЖНО: это стартовая гипотеза, не проверенное значение.** После первого
прогона на реальных walk-forward окнах (особенно на lean-universe
SBER/ROSN/NVTK, где сделок на окно объективно меньше) — проверить
распределение `numTrades` по всем trials и, если порог 20 отбраковывает
большинство конфигураций как "недостаточная выборка", пересмотреть в
меньшую сторону (например 10–15) или увеличить `train-months`. Занести
результат этой калибровки в README как отдельную заметку — это не разовая
настройка, а то, что может меняться при добавлении новых тикеров/периодов.

### 6. Учёт комиссии — критично

`VirtualExecutor` использует `virtualCommissionPerLot=5.0` только для расчёта
offset безубытка в трейлинге (см. `internal/engine/worker.go`), но НЕ вычитает
комиссию из итогового `gross_pnl` в экспорте сделок (это явно
задокументировано: *"gross_pnl в рублях, комиссия не вычтена"*). Поэтому
`objective.go` ОБЯЗАН сам вычитать комиссию за сделку при расчёте `Metrics` —
иначе поиск будет несправедливо в пользу конфигураций с частыми входами
(higher `maxEntriesPerTickerPerDay`, узкий `lookback`), которые в реальности
съедаются комиссией. Добавить флаг `-commission-per-trade` (default 5.0,
округлять по фактическому lot count сделки).

### 7. Вывод результатов

После завершения всех trials:
- JSON с полным списком (все trials + метрики) → `results/optimizer-run-{timestamp}.json`
- Top-5 конфигураций по Score в человекочитаемом виде в stdout
- Лучшая конфигурация — как готовый YAML в формате, совместимом с
  `internal/config` (тот же формат, что уже читает `cmd/bot`), сохранить в
  `results/best-config-{timestamp}.yaml`

**ВАЖНО:** optimizer только предлагает конфиг. Никакого auto-deploy в бота —
явный design decision, описать в README `cmd/optimizer`.

### 8. CLI интерфейс

```
optimizer run \
  -tickers SBER,ROSN,NVTK \
  -history-dir data/history \
  -search-space config/optimizer/search-space.yaml \
  -date-from 2024-01-01 -date-to 2026-06-01 \
  -train-months 6 -test-months 2 -step-months 1 \
  -trials 200 \
  -min-trades 20 \
  -commission-per-trade 5.0 \
  -output results/
```

### 9. Тесты

- unit-тест на `GenerateWindows` (границы, отсутствие overlap между train
  и своим test, корректный шаг)
- unit-тест на `Score` (отбраковка при малом `numTrades`, корректность Calmar)
- unit-тест на вычитание комиссии из `Metrics` (проверить, что PnL после
  комиссии отличается от `gross_pnl` `VirtualExecutor`)
- integration-тест на маленьком синтетическом датасете (несколько дней
  сгенерированных M5-свечей), проверяющий, что весь пайплайн
  Suggest → objective → Report → Best отрабатывает без паники

## Явные ограничения (не делать в этой итерации)

- НЕ реализовывать байесовскую/TPE оптимизацию — только Random Search
  с заделом на расширение
- НЕ трогать `cmd/bot/main.go` и `internal/engine/worker.go` рантайм-путь
- НЕ реализовывать auto-deploy найденного конфига
- НЕ оптимизировать категориальные параметры (`stopMode` range/atr,
  `tickerUniverse` lean/full) — они перебираются отдельными запусками CLI
  с разными флагами, не внутри одного search space
- НЕ трогать существующий баг `context.Background()` в
  `internal/engine/worker.go closePosition` — optimizer не исполняет
  реальные ордера и не использует `BCSClient` вообще
- НЕ трогать состав тикеров в существующих `configs/*.yaml` (MGNT/TATN
  и другие — оставить как есть, вне скоупа этой задачи)

## Definition of Done

- `cmd/optimizer` собирается и запускается отдельно от `cmd/bot` и `cmd/admin`
- Запуск на реальной истории по SBER/ROSN/NVTK за произвольный период
  отрабатывает без ошибок и производит `best-config` YAML
- Все unit/integration тесты проходят
- README в `cmd/optimizer` с описанием флагов, форматов входных/выходных
  файлов, учёта комиссии и design decision про отсутствие auto-deploy
