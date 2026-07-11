# Поиск оптимальных стратегий — рабочая методология

**Главный документ проекта.** Читать в начале любой сессии по optimizer.

Фокус: **walk-forward backtest** в `cmd/optimizer`. Бот и live — не приоритет, только для валидации гипотез.

Депозит: **200 000 ₽**, риск **0.5%** на сделку, сессия MOEX **10:00–18:40** MSK, M5.

## Навигация

| Задача | Документ / путь |
|--------|-----------------|
| Paper portfolio | `configs/runs/portfolio-paper.yaml` |
| Champion params | `configs/champions/*.yaml` |
| Код стратегий | [`strategies.md`](strategies.md) |
| Архив optimizer docs | [`legacy/README.md`](legacy/README.md) |
| Локальные прогоны | `results/*/runs-registry.json` |

---

## Найденные стратегии (3) — portfolio FROZEN

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
- Best config: `configs/champions/orc-wave2.yaml` (snapshot; исходный run: `results/orc/wave2/`)
- Search space: `configs/strategies/orc.yaml`
- Запуск: `make optimizer-orc`
- Подробнее: [`champion-orc.md`](champion-orc.md)

---

### 2. OR Fade — Opening Range Fade ✅ CHAMPION (FROZEN)

| | |
|---|---|
| ID | `opening_range_fade` |
| Статус | **FROZEN** — не оптимизировать без явного запроса |
| Принцип | Ложный пробой OR → **fade** (вход против пробоя) |
| Слот | ~10:15–12:30 |
| Champion run | **`wave1-conservative`** (seed 1) |
| Expectancy | **+0.25R** |
| PF | 1.49 |
| PnL | +33k / ~2 года |
| WF | **15/23** окон |
| Сделок | 133 |
| Тикеры | **LKOH, CHMF, MOEX** |

```
orb=16, breakout≈0.28%, fade_window=13, fade_end=77
atr≈1.98, RR≈1.19, require_inside_range=true
```

- Реестр: `results/or-fade/runs-registry.json`
- Best config: `configs/champions/or-fade-wave1-conservative.yaml`
- Tickers: `configs/shared/tickers-or-fade-conservative.yaml`
- Подробнее: [`champion-or-fade.md`](champion-or-fade.md)

**Complement к ORC:** разные принципы (continuation vs reversal) и слоты времени.

---

### 3. MF Afternoon — Momentum Filtered (12:30+) ✅ CHAMPION (FROZEN)

| | |
|---|---|
| ID | `momentum_filtered` |
| Статус | **FROZEN** — не оптимизировать без явного запроса |
| Принцип | Momentum breakout + SMA-тренд, **long-only**, входы с 12:30 |
| Слот | **12:30–18:40** (`session.entry_delay_minutes: 150`) |
| Champion run | **`mf-wave2-narrow`** (seed 1) |
| Expectancy | **+0.38R** |
| PF | 1.64 |
| PnL | +19.9k / ~2 года |
| WF | **14/23** окон |
| Сделок | 53 |
| Тикеры | **MGNT, TATN** (без ROSN) |

```
lookback=37, breakout≈0.94%, RR≈1.90, trendSMA≈32
strategyEntryDelay≈21, atr≈2.19, trail≈2.15R, long_only=true
```

- Реестр: `results/afternoon/runs-registry.json`
- Best config: `configs/champions/mf-afternoon-wave2-narrow.yaml`
- Tickers: `configs/shared/tickers-mf-afternoon-mgnt-tatn.yaml`
- Подробнее: [`champion-mf-afternoon.md`](champion-mf-afternoon.md)

---

## Портфель (целевая картина)

```
10:00 ─── 10:30 ─── 12:30 ─── 18:40
  │  ORC ✅   │ OR Fade ✅ │ MF Afternoon ✅ │
  │ MGNT/ROSN/ │ LKOH/CHMF/ │ MGNT/TATN      │
  │ TATN       │ MOEX       │                 │
```

**Portfolio FROZEN** — три champion-конфига, optimizer по ним не запускать без явного запроса.

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
| **wave2-narrow** | Search вокруг champion params | 300 | OR Fade wave2-narrow ✅ |

### Именование прогонов

```
ORC_RUN_ID=wave2          make optimizer-orc
ORF_RUN_ID=wave2-narrow   ORF_SEARCH_SPACE=configs/strategies/or-fade-wave2-narrow.yaml make optimizer-or-fade
AFT_STRATEGY=momentum_filtered AFT_RUN_ID=mf-wave1 make optimizer-afternoon
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
| Afternoon | `results/afternoon/runs-registry.json` | `make optimizer-afternoon` |
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
| `mean_reversion` (matrix) | −17k | без whitelist |
| `mean_reversion` afternoon (MGNT/ROSN/TATN) | exp −0.01R, −2.6k | **отклонено** 2026-07-11 |
| `momentum_filtered` (matrix) | −27k | без delay + whitelist |
| `momentum_filtered` afternoon | **+0.27R**, 14/23 | ✅ кандидат дневного слота |
| `momentum_breakout` afternoon (GAZP+CHMF) | exp −0.02R | **отклонено** 2026-07-11 |
| OR Fade wave1-expanded | **+0.78R** seed1 / +0.14R seed2 | новый universe LKOH/CHMF/MOEX |
| ORC на 9 тикеров | −280k | whitelist критичен |
| ORC + SBER | −4.6k…−10k | SBER исключён |
| ORC seed2 | exp +0.14R | overfit score, не champion |

Подробнее по momentum (архив): [`legacy/momentum-optimizer.md`](legacy/momentum-optimizer.md)

---

## Whitelist тикеров (текущий)

| Файл | Тикеры | Для чего |
|---|---|---|
| `tickers-orc-no-sber.yaml` | MGNT, ROSN, TATN | **ORC** champion |
| `tickers-or-fade-conservative.yaml` | LKOH, CHMF, MOEX | **OR Fade** champion |
| `tickers-mf-afternoon-mgnt-tatn.yaml` | MGNT, TATN | **MF Afternoon** champion |
| `tickers-or-fade-expanded.yaml` | LKOH, CHMF, TATN, GAZP, MOEX | архив matrix OR Fade |
| `tickers-mf-afternoon-expanded.yaml` | MGNT, SBER, NVTK, ROSN, TATN, GAZP | архив matrix MF |
| `tickers-orc.yaml` | + SBER | архив / сравнение |
| `tickers-momentum.yaml` | 9 тикеров | legacy momentum discovery |

**MGNT** — сильнейший тикер в ORC и OR Fade.

### Per-ticker matrix (2026-07-11)

Solo-run 100 trials на каждой акции. Артефакты: `results/ticker-matrix/*/summary.json`.

**MF Afternoon** (`momentum_filtered`, 12:30+):

| Тикер | exp_R | PF | WF | Вердикт |
|-------|------:|---:|---:|---------|
| MGNT | +0.47 | 2.29 | 13/23 | ✅ core |
| SBER | +0.22 | 1.40 | 11/23 | ✅ добавить |
| NVTK | +0.13 | 1.20 | 14/23 | ✅ добавить (153 сделки) |
| ROSN | +0.13 | 1.26 | 14/23 | ✅ |
| TATN | +0.11 | 1.21 | 13/23 | ✅ |
| GAZP | +0.09 | 1.16 | 11/23 | 🟡 слабо |
| CHMF | +0.14 | 1.16 | 2/23 | ❌ нестабильно |
| LKOH | −0.55 | 0.46 | 4/23 | ❌ |
| MOEX | −0.73 | 0.49 | 4/23 | ❌ |

→ Whitelist champion: `tickers-mf-afternoon-mgnt-tatn.yaml` (MGNT, TATN)

**ORC** (утро):

| Тикер | exp_R | PF | WF | Вердикт |
|-------|------:|---:|---:|---------|
| MGNT | +0.70 | 2.00 | 11/23 | ✅ core |
| ROSN | +0.38 | 1.49 | 14/23 | ✅ |
| LKOH | +0.33 | 1.37 | 7/23 | 🟡 мало WF |
| NVTK | +0.17 | 1.20 | 16/23 | 🟡 |
| TATN | +0.16 | 1.17 | 13/23 | ✅ |
| CHMF | +0.05 | 1.04 | 10/23 | ❌ слабо |
| GAZP | −0.06 | 0.94 | 12/23 | ❌ |
| MOEX | −0.27 | 0.74 | 7/23 | ❌ |
| SBER | −0.42 | 0.63 | 9/23 | ❌ |

→ Текущий whitelist MGNT/ROSN/TATN **подтверждён**.

**OR Fade** (утро–полдень):

| Тикер | exp_R | PF | WF | trades | Вердикт |
|-------|------:|---:|---:|-------:|---------|
| CHMF | +1.11 | 4.05 | 11/23 | 9 | 🟡 мало сделок |
| TATN | +0.98 | 3.41 | 8/23 | 12 | ✅ |
| LKOH | +0.64 | 2.37 | 13/23 | 39 | ✅ **новый** |
| MOEX | +0.30 | 1.42 | 8/23 | 14 | 🟡 |
| GAZP | +0.23 | 1.35 | 9/23 | 29 | 🟡 |
| SBER | +0.12 | 1.17 | 8/23 | 9 | 🟡 |
| MGNT | +0.07 | 1.11 | 10/23 | 52 | уже в портфеле |
| ROSN | −0.01 | 0.99 | 16/23 | 54 | ❌ для fade |
| NVTK | −0.32 | 0.61 | 11/23 | 24 | ❌ |

→ Whitelist: `tickers-or-fade-conservative.yaml` — код: **`ORFadeWhitelist`**

---

## Открытые задачи

- [x] OR Fade → **FROZEN** (`wave1-conservative`) — 2026-07-11
- [x] MF Afternoon → **FROZEN** (`mf-wave2-narrow`) — 2026-07-11
- [x] ORC → **FROZEN** (`wave2`) — ранее
- [x] Per-ticker matrix, per-strategy whitelist — **готово**
- [ ] Paper-валидация portfolio (`configs/runs/portfolio-paper.yaml`)
- [ ] Опционально: optimizатор по `expectancy_r`, а не Calmar score

---

## Быстрые команды

```bash
# История (при смене тикеров)
make sync-history

# Paper portfolio (3 FROZEN champions)
go run ./cmd/bot -config configs/runs/portfolio-paper.yaml

# ORC (frozen, только по запросу)
make optimizer-orc

# OR Fade (FROZEN — только по явному запросу)
# make optimizer-or-fade

# MF Afternoon (FROZEN — только по явному запросу)
# make optimizer-afternoon

# Сравнение прогонов — runs-registry.json или вывод после прогона
```

---

## Связанные документы

| Документ | Содержание |
|---|---|
| **Этот файл** | Методология, champions, статус исследований |
| [`champion-orc.md`](champion-orc.md) | ORC champion |
| [`champion-or-fade.md`](champion-or-fade.md) | OR Fade champion |
| [`champion-mf-afternoon.md`](champion-mf-afternoon.md) | MF Afternoon champion |
| [`strategies.md`](strategies.md) | Код стратегий, подключение в боте/optimizer |
| [`legacy/README.md`](legacy/README.md) | Архив optimizer-доков (закрытые линии) |

---

*Последнее обновление: 2026-07-11 (portfolio FROZEN)*
