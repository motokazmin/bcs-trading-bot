# Параметры search space и вопрос про тикеры

Куда смотреть в коде/конфигах:

- Search space: `configs/strategies/*.yaml` → блок `search_space`
- Зафиксированные чемпионы: `configs/champions/*.yaml`
- Наборы тикеров для прогонов: `configs/shared/tickers-*.yaml`
- Как искали: [`docs/optimizer-modes.md`](../optimizer-modes.md)

Ниже — **какие параметры реально крутили** при поиске, а не все поля YAML подряд.

---

## Словарь параметров (простым языком)

| Параметр в search space | В champion YAML | Смысл |
|---|---|---|
| `orbMinutes` | `orb_minutes` | Сколько минут строится Opening Range |
| `breakoutThreshold` | `breakout_threshold` | Насколько далеко цена должна уйти за край, чтобы считать пробой |
| `rewardRatio` | `reward_ratio` | Во сколько раз Take Profit дальше Stop Loss (примерно) |
| `atrMultiplier` | `atr_multiplier` | Насколько «широкий» Stop Loss относительно волатильности (ATR) |
| `trailActivationR` | `trail_activation_r` | После какого плюса (в R) включается подтягивание стопа |
| `trailStageMax` | `trail_stage_max` | Сколько ступеней у trailing |
| `trailBreakevenR` | `trail_breakeven_r` | Когда стоп подтягивается к безубытку |
| `trailDiscreteStepR` | `trail_discrete_step_r` | Шаг подтягивания стопа (у MF) |
| `maxEntriesPerTickerPerDay` | `max_trades_per_ticker_per_day` | Макс. входов на акцию за день |
| `fadeWindowMinutes` | `fade_window_minutes` | Сколько ждать возврата после пробоя (Fade) |
| `fadeTradeEndMinutes` | `fade_trade_end_minutes` | До какой минуты сессии ещё можно входить (Fade) |
| `requireInsideRange` | `require_inside_range` | Нужно ли цене зайти внутрь OR или достаточно вернуться за край |
| `lookback` | `lookback` | Сколько свечей для «недавнего диапазона» (MF) |
| `trendSMAPeriod` | `trend_sma_period` | Период средней для фильтра тренда (MF) |
| `strategyEntryDelayMinutes` | `strategy_entry_delay_minutes` | Доп. задержка входов внутри стратегии (MF) |
| `volumeFilter` | `volume_filter` | Включён ли фильтр по объёму |
| `volumeFilterMultiplier` | `volume_min_ratio` | Насколько объём должен быть выше обычного |

**Обычно не искали** (фиксировали снаружи):

- `atr_period: 14`
- `stop_mode: atr`
- риск: `risk_per_trade_percent: 0.5`, `max_daily_loss_percent: 2.0`
- у MF Afternoon в space: `longOnly: 1` (шорт выключен заранее)
- окно сессии (`session_open_time` / `eod_close_time` / `entry_delay_minutes`) — это слот портфеля, не trial-параметр

---

## По чемпионам: что искали

### 1. Утренний Session ORC

- Search space: `configs/strategies/session-orc-morning.yaml`
- Champion: `configs/champions/session-orc-morning-wave2.yaml`

| Параметр | Диапазон поиска | В чемпионе |
|---|---|---|
| `orbMinutes` | 10–25 | 14 |
| `breakoutThreshold` | 0.002–0.012 | ≈0.0038 |
| `rewardRatio` | 1.2–2.2 | ≈1.67 |
| `atrMultiplier` | 0.7–1.5 | ≈0.89 |
| `trailActivationR` | 1.0–2.2 | ≈1.07 |
| `trailStageMax` | 1–3 | 3 |
| `trailBreakevenR` | 0.0–0.15 | ≈0.05 |
| `maxEntriesPerTickerPerDay` | 1–2 | 1 |

Тикеры чемпиона: ROSN, NVTK, MOEX, CHMF, TATN, SBER

---

### 2. Main ORC

- Search space (wave2): `configs/strategies/orc-wave2.yaml`
- Champion: `configs/champions/orc-wave2.yaml`

| Параметр | Диапазон поиска | В чемпионе |
|---|---|---|
| `orbMinutes` | 12–22 | 15 |
| `breakoutThreshold` | 0.003–0.015 | ≈0.0084 |
| `rewardRatio` | 1.3–2.0 | ≈1.64 |
| `atrMultiplier` | 0.8–1.3 | ≈0.84 |
| `trailActivationR` | 1.5–2.2 | ≈1.52 |
| `trailStageMax` | 1–2 | 1 |
| `trailBreakevenR` | 0.0–0.15 | ≈0.014 |
| `maxEntriesPerTickerPerDay` | 1–3 | 1 |

Тикеры чемпиона: MGNT, ROSN, TATN  
(в wave2 search YAML ещё фигурировал SBER — в финальный champion не вошёл)

---

### 3. OR Fade

- Search space (база): `configs/strategies/or-fade.yaml`
- Champion: `configs/champions/or-fade-wave3-afks.yaml`

| Параметр | Диапазон поиска | В чемпионе |
|---|---|---|
| `orbMinutes` | 10–25 | 26 |
| `breakoutThreshold` | 0.0–0.02 | ≈0.0035 |
| `fadeWindowMinutes` | 10–60 | 52 |
| `fadeTradeEndMinutes` | 45–150 | 106 |
| `requireInsideRange` | 0–1 | false |
| `rewardRatio` | 1.0–2.0 | ≈1.27 |
| `atrMultiplier` | 0.8–2.0 | ≈1.23 |
| `trailActivationR` | 1.0–2.5 | ≈2.00 |
| `trailStageMax` | 1–3 | 3 |
| `trailBreakevenR` | 0.0–0.15 | ≈0.018 |
| `maxEntriesPerTickerPerDay` | 1–3 | 2 |

Тикеры чемпиона: LKOH, CHMF, MOEX, AFKS

Замечание: у чемпиона `orb_minutes: 26` чуть **выше** max=25 базового `or-fade.yaml` —
финальный snapshot мог прийти из более позднего/узкого прогона (wave2/wave3),
а не один-в-один из базового space. Сверять с конкретным прогоном, если нужна
полная трассировка.

---

### 4. MF Afternoon

- Широкий space: `configs/strategies/momentum-filtered-afternoon.yaml`
- Узкий (longonly-narrow): `...-longonly-narrow-ws2.yaml`
- Champion: `configs/champions/mf-afternoon-reopt-s2.yaml`

| Параметр | Широкий поиск | Узкий (пример) | В чемпионе |
|---|---|---|---|
| `lookback` | 10–40 | 32–40 | 39 |
| `breakoutThreshold` | 0.0–0.05 | 0.0062–0.0083 | ≈0.0077 |
| `rewardRatio` | 1.0–3.0 | 1.07–1.45 | ≈1.39 |
| `volumeFilter` | 0–1 | 0–1 | false |
| `volumeFilterMultiplier` | 1.0–3.0 | 1.0–1.5 | ≈1.28 (не используется при filter=false) |
| `trendSMAPeriod` | 0–50 | 28–38 | 37 |
| `strategyEntryDelayMinutes` | 0–30 | 22–30 | 26 |
| `atrMultiplier` | 1.0–3.0 | 2.5–3.0 | ≈2.66 |
| `trailActivationR` | 1.0–3.0 | 2.5–3.0 | ≈2.52 |
| `trailDiscreteStepR` | 0.25–1.0 | 0.28–0.40 | ≈0.29 |
| `trailStageMax` | 1–5 | 1–3 | 2 |
| `maxEntriesPerTickerPerDay` | 1–5 | 1–3 | 2 |

Fixed в space: `longOnly: 1`  
Слот: `entry_delay_minutes: 150` → входы с ~12:30  
Тикеры чемпиона: MGNT, TATN

---

### 5. Вечерний Session ORC

- Search space: `configs/strategies/session-orc-evening.yaml`
- Champion: `configs/champions/session-orc-evening-wave2.yaml`

| Параметр | Диапазон поиска | В чемпионе |
|---|---|---|
| `orbMinutes` | 10–25 | 11 |
| `breakoutThreshold` | 0.002–0.012 | ≈0.0055 |
| `rewardRatio` | 1.2–2.2 | ≈1.58 |
| `atrMultiplier` | 0.7–1.5 | ≈1.21 |
| `trailActivationR` | 1.0–2.2 | ≈2.05 |
| `trailStageMax` | 1–3 | 1 |
| `trailBreakevenR` | 0.0–0.15 | *(в champion не задан явно)* |
| `maxEntriesPerTickerPerDay` | 1–2 | 2 |

Тикеры чемпиона: NVTK, GAZP, ROSN, CHMF, MOEX, TATN, MGNT

---

## Почему стратегии не на всех тикерах — и почему это может быть хрупко

Короткий ответ: **не потому что стратегия «физически» умеет работать только на MGNT**,  
а потому что набор бумаг **сужали по результатам прогонов** (matrix / wave1→wave2→champion).

Как это выглядит у вас:

1. Optimizer крутит params на выбранном universe тикеров.
2. Потом смотрят, где edge держится лучше / хуже.
3. Слабые бумаги выкидывают из слота (`configs/shared/tickers-*.yaml`, комментарии вроде «drop MGNT/GAZP/LKOH»).
4. В champion YAML остаётся узкий список.
5. Тикеры теперь управляются **только через YAML** (`experiments[].tickers:` в
   `configs/runs/portfolio-paper.yaml`). С переходом на StrategyRunner/SelfManagedStrategy
   (ADR 0001, Фазы 4–5) code-level whitelist/blacklist удалён из движка.

### Согласие с сомнением

Для таких простых правил (OR / momentum) «магическая привязка к 2–3 тикерам»
часто **не отражает уникальные свойства бумаги**, а отражает:

- случайность выборки на истории;
- overfit params + universe вместе;
- разную ликвидность/шум, который меняется со временем;
- эффект «на этих бумагах trial набрал score, на других — нет».

То есть узкий список — это **операционное решение портфеля**
(меньше шума, лучше выглядел backtest), а не доказательство, что
«ROSN по природе ORC-бумага, а SBER — нет».

В `optimizer-modes.md` прямо есть предупреждение: не делать основным режимом
подбор «одна стратегия — одна акция» — шум и overfit.

### Что из этого следует

| Подход | Плюс | Минус |
|---|---|---|
| Узкий whitelist как сейчас | чище метрики на истории, меньше мусорных сделок | хрупко при смене режима рынка / ликвидности |
| Одна логика на широкий universe | ближе к «свойству стратегии», а не бумаги | больше шума, params могут «средне» работать везде |
| Одинаковые params, разные тикеры как stress-test | честная проверка устойчивости | может убить красивый baseline |

Практичный вывод: тикерный список чемпиона лучше считать **гипотезой, которую надо периодически перепроверять**, а не истиной про инструмент. Params искали в space выше; universe — отдельный рычаг, и он так же подвержен переоптимизации.

---

## Что сознательно не входит в этот документ

- Полная история всех wave/reopt прогонов и seed
- Почему конкретное число (например 0.0084) «лучше» соседнего
- Нужно ли сейчас расширять universe — это уже решение по исследованию, не описание факта
