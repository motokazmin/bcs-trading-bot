# Поиск оптимальных стратегий — рабочая методология

**Главный документ проекта.** Читать в начале любой сессии по optimizer.

Фокус: **walk-forward backtest** в `cmd/optimizer`. Бот и live — не приоритет, только для валидации гипотез.

Депозит: **200 000 ₽**, риск **0.5%** на сделку, сессия MOEX **10:00–18:40** MSK, M5.

---

## Найденные стратегии (2)

### 1. ORC — Opening Range Continuation ✅ CHAMPION (FROZEN)

| | |
|---|---|
| ID | `opening_range_continuation` |
| Статус | **FROZEN** — не оптимизировать без явного запроса |
| Принцип | Пробой утреннего OR → **вход лимитом на ретесте** (continuation) |
| Слот | ~10:00–10:30 |
| Champion run | `wave2` (seed 1) |
| Expectancy | **+0.51R** |
| PF | 1.62 |
| PnL | +57 733 ₽ / 2 года |
| WF | 14/23 окон |
| Сделок | 113 |
| Тикеры | **MGNT, ROSN, TATN** (без SBER) |

```
orb=13, atr≈0.83, RR≈1.47, breakout≈0.86%
trail_act≈2.04, max_entries=2
```

- Реестр: `results/orc/runs-registry.json`
- Best config: `results/orc/wave2/best-config-20260710-153944.yaml`
- Search space: `configs/strategies/orc.yaml`
- Запуск: `make optimizer-orc`
- Подробнее: [`champion-orc.md`](champion-orc.md), [`orc-optimizer.md`](orc-optimizer.md)

---

### 2. OR Fade — Opening Range Fade 🟡 CHAMPION (предварительный)

| | |
|---|---|
| ID | `opening_range_fade` |
| Статус | **предварительный champion** — нужен narrow search + финальная валидация seed |
| Принцип | Ложный пробой OR → **fade** (вход против пробоя) |
| Слот | ~10:15–12:30 (`fadeTradeEndMinutes≈138`) |
| Лучший run | `wave1` (seed 1), conservative: `wave1-seed2` |
| Expectancy | +0.86R (seed1, 30 сделок ⚠️) / **+0.28R** (seed2, 106 сделок) |
| PF | 2.99 / 1.40 |
| PnL | +26k / **+29k** |
| WF | 18/23 / 17/23 |
| Тикеры | **MGNT, ROSN, TATN** |

```
orb≈11, breakout≈0.5%, fade_window≈44, fade_end≈138
atr≈0.95, RR≈1.77, require_inside_range=false
```

- Реестр: `results/or-fade/runs-registry.json`
- Код: `internal/strategy/opening_range_fade.go`
- Search space: `configs/strategies/or-fade.yaml`
- Запуск: `make optimizer-or-fade`
- Подробнее: [`or-fade-optimizer.md`](or-fade-optimizer.md)

**Complement к ORC:** разные принципы (continuation vs reversal) и слоты времени.

---

## Портфель (целевая картина)

```
10:00 ─── 10:30 ─── 12:30 ─── 18:40
  │  ORC ✅   │ OR Fade 🟡 │  (свободно) │
  │continuation│   fade    │  дневной слот │
```

Третья стратегия — на слот **12:30–18:40** (ещё не найдена).

---

## Как мы работаем (workflow)

### Роли

- **Агент** — реализует стратегии, готовит конфиги, **сам запускает** optimizer, анализирует, пишет в реестр. Пользователю сообщает **только результаты и выводы**.
- **Пользователь** — задаёт направление («попробуем OR Fade», «ищем дневную»), принимает решения о заморозке champion.

### Цикл одного эксперимента

```
1. Гипотеза (принцип + слот + тикеры)
2. Search space YAML (configs/strategies/<name>.yaml)
3. Tickers YAML (configs/shared/tickers-*.yaml) — формат строк, не объектов!
4. Запуск optimizer (make target или scripts/run-*-optimizer.sh)
5. Автозапись в runs-registry.json (scripts/record-strategy-run.py)
6. Сравнение: expectancy_r, PF, WF окон, by_ticker
7. Решение: champion / отклонить / следующая волна
```

### Волны (waves)

| Wave | Цель | Trials | Пример |
|---|---|---:|---|
| **wave1-wide** | Discovery: широкий search, 9 тикеров | 200 | ORC wave1-wide |
| **wave2** | Узкий search вокруг best | 300 | ORC wave2 |
| **wave3-*** | Whitelist (убрать минусовые тикеры) | 300 | ORC wave3-no-sber |
| **seed2** | Проверка стабильности | 300 | ORC wave2-seed2, OR Fade wave1-seed2 |
| **wave2-narrow** | Search вокруг champion params | 300 | (TODO для OR Fade) |

### Именование прогонов

```
ORC_RUN_ID=wave2          make optimizer-orc
ORF_RUN_ID=wave1-seed2    ORF_SEED=2 make optimizer-or-fade
MOM_RUN_ID=wave1-wide     make optimizer-momentum
```

Артефакты каждого прогона:
- `results/<strategy>/<run_id>/optimizer-run-*.json`
- `results/<strategy>/<run_id>/best-config-*.yaml`
- `results/<strategy>/<run_id>/export/data-trades.json` (поле `strategy_params` на сделке)
- `results/<strategy>/runs-registry.json`

---

## Критерии «стратегия найдена»

Главная метрика — **`expectancy_r`** (средний PnL в R на сделку), **не** walk-forward score (Calmar).

| Критерий | Минимум для champion | Комментарий |
|---|---|---|
| `expectancy_r` | > 0, лучше > +0.15R | ORC: +0.51R — эталон |
| `profit_factor` | > 1.3 | |
| `profitable_windows` | ≥ 10/23 | из 23 WF-окон ~2 года |
| Сделок | достаточно для статистики | ORC: 113; OR Fade seed2: 106; <30 — осторожно |
| Seed2 | не хуже сильно seed1 | ORC seed2 провалился; OR Fade seed2 ок |
| Whitelist | обязателен | 9 тикеров без фильтра почти всегда минус |
| Score (Calmar) | игнорировать как единственный | может быть высоким при минусе в ₽ |

---

## Реестры прогонов

| Стратегия | Реестр | Makefile |
|---|---|---|
| ORC | `results/orc/runs-registry.json` | `make optimizer-orc` |
| OR Fade | `results/or-fade/runs-registry.json` | `make optimizer-or-fade` |
| Momentum | `results/momentum/runs-registry.json` | `make optimizer-momentum` |

Универсальный recorder: `scripts/record-strategy-run.py`

---

## Что пробовали и отклонили

| Стратегия / линия | Результат | Вывод |
|---|---|---|
| `opening_range` (market ORB) | −31k, 4/23 | хуже ORC; шорты мертвы |
| `momentum_breakout` (9 тикеров) | −12k, exp −0.07R | нет edge |
| `momentum_breakout` (lean SBER/ROSN/NVTK) | −18k | live ≠ backtest |
| `momentum_breakout` (GAZP+CHMF) | +0.05R | слишком слабо |
| `mean_reversion` (matrix) | −17k | не тестировали whitelist |
| `momentum_filtered` (matrix) | −27k | не тестировали с delay + whitelist |
| ORC на 9 тикеров | −280k | whitelist критичен |
| ORC + SBER | −4.6k…−10k | SBER исключён |
| ORC seed2 | exp +0.14R | overfit score, не champion |

Подробнее по momentum: [`momentum-optimizer.md`](momentum-optimizer.md)

---

## Whitelist тикеров (текущий)

| Файл | Тикеры | Для чего |
|---|---|---|
| `tickers-orc-no-sber.yaml` | MGNT, ROSN, TATN | **рабочий** для ORC и OR Fade |
| `tickers-orc.yaml` | + SBER | архив / сравнение |
| `tickers-momentum.yaml` | 9 тикеров | discovery momentum (заморожено) |

**MGNT** — сильнейший тикер в ORC и OR Fade.

---

## Открытые задачи

- [ ] OR Fade: **wave2-narrow** вокруг wave1 best + seed2 валидация → заморозить champion
- [ ] OR Fade: зафиксировать conservative baseline по seed2 params
- [ ] Третья стратегия на слот **12:30–18:40** (mean reversion / momentum_filtered с delay — кандидаты)
- [ ] Опционально: оптимизатор по `expectancy_r`, а не Calmar score

---

## Быстрые команды

```bash
# История (при смене тикеров)
make sync-history

# ORC (frozen, только по запросу)
make optimizer-orc

# OR Fade
make optimizer-or-fade

# Momentum (заморожено)
make optimizer-momentum

# Сравнение прогонов — смотреть runs-registry.json или вывод в консоли после прогона
```

---

## Связанные документы

| Документ | Содержание |
|---|---|
| **Этот файл** | Методология, champions, статус исследований |
| [`champion-orc.md`](champion-orc.md) | Детали ORC champion |
| [`orc-optimizer.md`](orc-optimizer.md) | ORC runs, конфиги |
| [`or-fade-optimizer.md`](or-fade-optimizer.md) | OR Fade runs, конфиги |
| [`momentum-optimizer.md`](momentum-optimizer.md) | Momentum (отклонено) |
| [`strategies.md`](strategies.md) | Техническое описание всех стратегий в коде |

---

*Последнее обновление: 2026-07-10*
