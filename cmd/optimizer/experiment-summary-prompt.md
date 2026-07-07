# Промпт: результаты экспериментов bcs-trading-bot optimizer

Контекст для следующей сессии / задачи. Обновлять после новых прогонов matrix.

---

## Контекст проекта

Репозиторий: **bcs-trading-bot** — Go-бот для MOEX (BCS Trade API), M5, paper/real.

Добавлен offline-сервис **`cmd/optimizer`**: walk-forward backtest + Random Search (200 trials).
Использует тот же код стратегий и `internal/simulation.Runner` ≈ live `TickerWorker`.
**Auto-deploy нет** — `best-config.yaml` применяется вручную.

## Методология экспериментов

- **Данные:** ~2 года M5 CSV (`data/history/`), 9 акций: SBER, GAZP, LKOH, NVTK, ROSN, MGNT, CHMF, TATN, MOEX
- **Walk-forward:** окна по 2 мес, шаг 1 мес (`-window-months` / `-step-months`)
- **Поиск:** Random Search, 200 trials, `seed=1`, `min-trades=10` (в matrix)
- **Scoring:** median score по окнам; убыток → score = PnL (руб.), прибыль → Calmar; комиссия 5₽/лот вычитается
- **Вне search space:** `stop_mode` (atr/range), universe — отдельные CLI-запуски

Запуск:

- `make strategy-matrix` — 4 стратегии подряд (`scripts/run-strategy-matrix.sh`)

## Что реализовано (инфраструктура)

- Optimizer: sync-history, walk-forward, 4 стратегии, parallel trials, преднарезка окон, опциональный `-two-phase`
- Ускорение подтверждено (06.07): вся matrix **~22 мин** при `parallel=28` (было 2+ часа)
- Документация: `cmd/optimizer/README.md` (подход + архитектура)

## Результаты strategy-matrix (актуальный прогон 2026-07-06)

| Стратегия | score (median) | Суммарный PnL | Сделок | Окон в плюсе |
|-----------|----------------|---------------|--------|--------------|
| mean_reversion | **+0.017** | **−70 056 ₽** | 153 | 5/17 |
| opening_range | −14 763 | −48 026 ₽ | 41 | 0/17 |
| momentum_filtered | −10 126 | −80 521 ₽ | 67 | 1/17 |
| momentum_breakout | −14 875 | −91 135 ₽ | 72 | 2/17 |

> Прогон 2026-07-06 — старая схема train/test. Перезапустить после смены методологии.

Артефакты: `results/strategy-matrix-summary.json`, `results/strategy-matrix-run.log`.

## Ключевые выводы

1. **Устойчивого edge нет** — ни одна стратегия не дала положительного суммарного OOS PnL за 2 года на 9 акциях.
2. **Train тоже в минусе** — это не переобучение, а отсутствие альфы в текущих гипотезах.
3. **mean_reversion с test_score > 0 — ловушка метрики:** медиана Calmar по 3 слабым прибыльным окнам (+0.017), но суммарный OOS **−70k**; WARN не сработал (проверка только `test_score < 0`). Смотреть на **sum_test_pnl** и **profitable_test_windows**.
4. **Лучший по рублям** среди 4 стратегий — opening_range (−48k), но 0/17 прибыльных окон → нестабильно.
5. **range stop** хуже ATR; после фикса `rangeUseCap` старые range-прогоны нужно пересчитать.
6. **best-config не для деплоя** — только «наименее плохой» набор параметров.

## Известные ограничения / техдолг

- WARN при убыточном OOS должен учитывать `sum_test_pnl < 0`, не только `test_score < 0`
- Scoring: смешение Calmar (на плюсовых окнах) и PnL (на минусовых) искажает медиану
- Random Search 200 trials — baseline; TPE не реализован
- Backtest: fill по close, без проскальзывания

## Рекомендуемое направление (не делать слепо)

- **Не** ещё 500 trials в том же search space
- **Да:** смена торговой гипотезы (новая логика / фильтр режима рынка)
- **Да:** разбор убытка по тикерам и окнам (кто тянет вниз)
- **Да:** paper только как контроль «live ≠ backtest», без ожидания чуда
- Опционально: перезапуск range-экспериментов после фикса `rangeUseCap`

## Задача для агента (шаблон)

```
Контекст: bcs-trading-bot, optimizer walk-forward завершён.
Все 4 стратегии (momentum_breakout, momentum_filtered, opening_range, mean_reversion)
на 9 акциях MOEX 2024-07 — 2026-07: OOS суммарно убыточен.
mean_reversion: test_score=+0.017, но sum_test_pnl=-70k (артефакт медианы Calmar).

[Выбери одно:]
1. Разобрать JSON mean_reversion по тикерам/окнам — кто даёт +3215 и кто −18k
2. Предложить новую стратегию / фильтр режима с обоснованием
3. Исправить WARN optimizer: предупреждать при sum_test_pnl < 0
4. Спроектировать scoring без ложных «плюсов» при отрицательном портфеле
```
