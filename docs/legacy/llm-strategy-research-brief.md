# BCS Trading Bot — полный отчёт по стратегиям и испытаниям (для анализа LLM)

Документ самодостаточный: идеи стратегий, конкретные параметры, метрики walk-forward и вердикты. Внешние файлы не нужны.

**Статус архива (после 2026-07-16):** frequency/whitelist research закрыт. На ту дату в paper оставались **3 FROZEN champions** main-сессии (ORC, OR Fade без TATN, MF Afternoon). Candidates (OR Fade+TATN, VWAP MGNT+ROSN) **не** влиты. После 2026-07-17 paper = **5 champions** (+ Morning/Evening Session ORC) — см. [`strategy-research.md`](../strategy-research.md).

**Дата среза:** 2026-07-16  
**Рынок:** акции MOEX, класс TQBR, таймфрейм M5  
**Сессия:** 10:00–18:40 Europe/Moscow  
**Депозит backtest:** 200 000 ₽  
**Риск на сделку:** 0,5% депозита  
**Дневной circuit breaker:** 2% депозита  
**Комиссия модели:** 0,008% от оборота за каждый leg (вход + выход)  
**Исполнение в backtest:** fill по close свечи, без проскальзывания  
**Оптимизация:** Random Search + walk-forward (окна ~2 мес, шаг 1 мес → 23 окна на ~2 года истории)  
**Главная метрика решений:** `expectancy_r` (средний PnL в единицах риска R на сделку), не Calmar score  

### Пороги champion (go/no-go)

| Критерий | Минимум |
|----------|---------|
| expectancy_r | > 0, лучше > +0,15R |
| profit_factor (PF) | > 1,3 |
| profitable_windows (WF) | ≥ 10 из 23 |
| Число сделок | достаточно для статистики (осторожно при <30 за 2 года) |
| seed2 | не сильно хуже seed1 |
| Whitelist тикеров | обязателен; 9 тикеров без фильтра почти всегда минус |

**Важно:** положительный Calmar/score при отрицательном суммарном PnL в рублях — не champion.

---

## 1. Портфель FROZEN (production / paper baseline)

Три независимые стратегии на разных слотах времени. Оценка портфеля = сумма трёх отдельных backtest на полном депозите 200k (не единый portfolio-sim).

```
10:00 ──────── 10:30 ──────── 12:30 ──────────────── 18:40
   ORC              OR Fade           MF Afternoon
   MGNT/ROSN/TATN   LKOH/CHMF/        MGNT/TATN
                    MOEX/AFKS
```

| Метрика портфеля (оценка) | Значение |
|---------------------------|----------|
| Суммарный net PnL (~2 года) | +110 047 ₽ (в одном источнике также фигурирует оценка ~+76 669 ₽ при другой агрегации доли; ниже — покомпонентно) |
| Сделок всего | 266 (~0,5 / торг. день, ~11 / мес) |
| Средневзвешенный exp_R | +0,42R = (0,49×110 + 0,50×89 + 0,18×67) / 266 |
| Узкое место | частота сделок, не отсутствие edge |

### Покомпонентный baseline (commission-rerun, seed 1)

| Стратегия | Run ID | Сделок | WR | exp_R | PF | Net PnL | ~%/год* | WF |
|-----------|--------|-------:|---:|------:|---:|--------:|--------:|---:|
| ORC | wave2-rerun | 110 | 55,5% | +0,49R | 1,61 | +53 559 ₽ | ~13% | 16/23 |
| OR Fade | wave3-narrow-afks | 89 | 59,6% | +0,50R | 1,99 | +44 514 ₽ | ~11% | 18/23 |
| MF Afternoon | mf-wave2-narrow-rerun | 67 | 49,3% | +0,18R | 1,34 | +11 974 ₽ | ~3% | 13/23 |

\* Простая annualization (return за ~2 года / 2), не CAGR.

ORC даёт основную долю PnL; OR Fade и MF — хороший exp_R при меньшей частоте.

---

## 2. Champion 1: ORC — Opening Range Continuation

### Идея

Утренний opening range (первые N минут сессии) задаёт диапазон. При пробое диапазона с порогом стратегия **не входит market**, а ставит **лимит на ретест** границы (continuation по тренду пробоя). Стоп — ATR; тейк — reward_ratio × риск; трейлинг после активации.

Это **не** сырой market-ORB (та линия отклонена: −31k, 4/23 WF).

### Слот и universe

- Слот: ~10:00–10:30 MSK  
- Тикеры FROZEN: **MGNT, ROSN, TATN** (SBER исключён — стабильный минус)  
- max_trades_per_ticker_per_day: 1  

### Конкретные параметры champion (wave2-rerun, seed 1)

| Параметр | Значение |
|----------|----------|
| strategy.type | opening_range_continuation |
| stop_mode | atr |
| atr_period | 14 |
| orb_minutes | 15 |
| atr_multiplier | 0,84124 |
| breakout_threshold | 0,008382 (~0,84%) |
| reward_ratio | 1,641 |
| trail_activation_r | 1,515 |
| trail_breakeven_r | 0,01395 |
| trail_stage_max | 1 |
| max_trades_per_ticker_per_day | 1 |
| risk_per_trade_percent | 0,5 |
| max_daily_loss_percent | 2,0 |

### Результаты champion

| Метрика | Значение |
|---------|----------|
| expectancy_r | +0,49R (~+487 ₽/сделку) |
| profit_factor | 1,61 |
| win_rate | 55,5% |
| trades | 110 |
| net PnL | +53 559 ₽ / ~2 года |
| WF | 16/23 |
| Seed2 (историческая валидация в champion-доке) | +0,54R, 107 сделок, 16/23 — ок для FROZEN; отдельный ORC seed2 на ранней волне wave2 давал overfit score при exp +0,14R — не путать |

### Связанные отклонения по ORC

| Линия | Результат | Вывод |
|-------|-----------|-------|
| ORC на 9 тикерах | −280k | whitelist критичен |
| ORC + SBER | −4,6k…−10k | SBER исключён |
| ORC LKOH solo (2026-07-16) | seed1 +0,52R PF 1,90 16/23; seed2 +0,23R **PF 1,25** | отклонено: seed2 PF < 1,3 |
| opening_range (market ORB) | −31k, 4/23 | хуже ORC |

---

## 3. Champion 2: OR Fade — Opening Range Fade

### Идея

Ложный пробой утреннего OR → **fade** (вход против направления ложного пробоя, возврат в диапазон). Параметры окна fade и конца торговли задают утренний–полуденный слот. Complement к ORC: другой принцип (reversal vs continuation) и другие тикеры.

### Слот и universe

- Слот: ~10:15–12:30 (fade_trade_end_minutes ≈ 106 мин после 10:00 → ~11:46)  
- Тикеры FROZEN: **LKOH, CHMF, MOEX, AFKS**  
- Candidate (см. §5): те же params + **TATN**  

### Конкретные параметры champion (wave3-narrow-afks, seed 1)

| Параметр | Значение |
|----------|----------|
| strategy.type | opening_range_fade |
| stop_mode | atr |
| atr_period | 14 |
| orb_minutes | 26 |
| breakout_threshold | 0,003490 (~0,35%) |
| fade_window_minutes | 52 |
| fade_trade_end_minutes | 106 |
| require_inside_range | false |
| atr_multiplier | 1,232 |
| reward_ratio | 1,271 |
| trail_activation_r | 2,004 |
| trail_breakeven_r | 0,01772 |
| trail_stage_max | 3 |
| max_trades_per_ticker_per_day | 2 |
| risk_per_trade_percent | 0,5 |

### Результаты champion

| Метрика | Значение |
|---------|----------|
| expectancy_r | +0,50R (~+500 ₽/сделку) |
| profit_factor | 1,99 |
| win_rate | 59,6% |
| trades | 89 |
| net PnL | +44 514 ₽ / ~2 года |
| WF | 18/23 |

Предшественник без AFKS (`wave1-conservative-rerun`): +0,33R, 34 сделки, 13/23.

### Ablation частоты: max_entries 1 / 2 / 3 (2026-07-16)

Гипотеза «второй вход увеличит частоту». Champion уже имел max_trades=2. При **тех же** champion-параметрах и universe LKOH/CHMF/MOEX/AFKS:

| maxEntries | exp_R | PF | WF | trades | PnL |
|------------|------:|---:|---:|-------:|----:|
| 1 | +0,51 | 2,01 | 18/23 | 89 | +44,9k |
| 2 | +0,50 | 1,99 | 18/23 | 89 | +44,5k |
| 3 | +0,50 | 1,99 | 18/23 | 89 | +44,5k |

**Вывод:** повторный fade в слоте почти не возникает; увеличение max_entries **не** даёт частоты. Отклонено как рычаг частоты.

---

## 4. Champion 3: MF Afternoon — Momentum Filtered

### Идея

Пробой high/low за lookback баров M5 + фильтр тренда по SMA + volume filter, **только long**, входы не раньше ~12:30 (session entry_delay 150 мин + strategy_entry_delay).

Без afternoon delay и без узкого whitelist стратегия на широком universe убыточна (matrix −27k).

### Слот и universe

- Слот: 12:30–18:40  
- Тикеры FROZEN: **MGNT, TATN**  

### Конкретные параметры champion (mf-wave2-narrow-rerun, seed 1)

| Параметр | Значение |
|----------|----------|
| strategy.type | momentum_filtered |
| stop_mode | atr |
| atr_period | 14 |
| lookback | 31 |
| breakout_threshold | 0,008331 (~0,83%) |
| reward_ratio | 1,838 |
| volume_filter | true |
| volume_min_ratio | 2,486 |
| long_only | true |
| trend_sma_period | 20 |
| strategy_entry_delay_minutes | 24 |
| atr_multiplier | 2,727 |
| trail_activation_r | 2,400 |
| trail_discrete_step_r | 0,664 |
| trail_stage_max | 3 |
| max_trades_per_ticker_per_day | 1 |
| session.entry_delay_minutes | 150 |
| risk_per_trade_percent | 0,5 |

### Результаты champion

| Метрика | Значение |
|---------|----------|
| expectancy_r | +0,18R (~+179 ₽/сделку) |
| profit_factor | 1,34 |
| win_rate | 49,3% |
| trades | 67 |
| net PnL | +11 974 ₽ / ~2 года |
| WF | 13/23 |
| Seed2 (в champion-доке) | +0,18R, 62 сделки, 14/23 |

### Per-ticker matrix MF (2026-07-11, solo 100 trials) — контекст

| Тикер | exp_R | PF | WF | Вердикт matrix |
|-------|------:|---:|---:|----------------|
| MGNT | +0,47 | 2,29 | 13/23 | core |
| SBER | +0,22 | 1,40 | 11/23 | казался живым |
| NVTK | +0,13 | 1,20 | 14/23 | слабоват PF |
| ROSN | +0,13 | 1,26 | 14/23 | слабоват |
| TATN | +0,11 | 1,21 | 13/23 | в champion |
| GAZP | +0,09 | 1,16 | 11/23 | слабо |
| CHMF | +0,14 | 1,16 | 2/23 | нестабильно |
| LKOH | −0,55 | 0,46 | 4/23 | нет |
| MOEX | −0,73 | 0,49 | 4/23 | нет |

**Повторная проверка solo 2026-07-16** (тот же principle, afternoon search): SBER/NVTK/ROSN — все exp_R < 0. Matrix-edge **не переносится**; расширение MF whitelist отклонено.

| Run | exp_R | PF | WF | trades | PnL | Вердикт |
|-----|------:|---:|---:|-------:|----:|---------|
| mf-solo-sber (narrow) | — | — | 0/23 | 0 | 0 | 0 сделок |
| mf-solo-sber-wide | −0,11 | 0,83 | 7/23 | 22 | −2,3k | отклонено |
| mf-solo-nvtk | −0,16 | 0,77 | 8/23 | 92 | −14,4k | отклонено |
| mf-solo-rosn | −0,19 | 0,73 | 7/23 | 51 | −9,5k | отклонено |

Отдельно отклонена линия `momentum_sber_daytrend` (momentum + фильтр дня по open vs prev close): +0,01R, PF 1,01, Calmar+ при ~нулевом PnL.

---

## 5. Candidate: OR Fade + TATN (фиксированные champion-параметры)

### Идея

Не новый principle и не перетюнинг. Solo TATN на OR Fade прошёл seed1/seed2; добавление TATN к FROZEN params без изменения гиперпараметров.

### Параметры

**Идентичны** §3 (wave3-narrow-afks). Universe: LKOH, CHMF, MOEX, AFKS, **TATN**.

### Результаты

| Run | Суть | exp_R | PF | WF | trades | PnL | Вердикт |
|-----|------|------:|---:|---:|-------:|----:|---------|
| tatn-solo | только TATN, wave2-narrow search, seed1 | +0,38 | 1,51 | 14/23 | 49 | +18,6k | ok |
| tatn-solo-seed2 | seed2 | +0,28 | 1,36 | 13/23 | 37 | +10,3k | ok |
| wave3-plus-tatn | re-tune search на 5 тикерах, seed1 | +1,02 | 3,32 | 10/23 | **22** | +22,4k | мало сделок — не брать |
| wave3-plus-tatn-seed2 | re-tune seed2 | +0,57 | 2,00 | 12/23 | 24 | +13,7k | мало сделок |
| **fixed-plus-tatn** | **те же params что champion, +TATN** | **+0,48** | **1,94** | **19/23** | **103** | **+48,8k** | **GO candidate** |

Из 103 сделок: TATN дал 17 сделок и ~+9k ₽. WF улучшился 18→19/23; exp_R чуть ниже champion (+0,48 vs +0,50), частота выше.

**Предупреждение для portfolio:** TATN уже торгуется в ORC (утро) и MF (день). Нужно правило приоритета одной позиции на тикер перед влитием в paper.

---

## 6. Candidate: VWAP Pullback Continuation (MGNT + ROSN)

### Идея

1. Утром строится OR (`orb_minutes`) и фиксируется направление пробоя (`breakout_threshold`) — как direction filter (не mean reversion).  
2. Считается session VWAP по M5 (typical price × volume).  
3. После `strategy_entry_delay_minutes` от open: цена должна держаться выше VWAP ≥ `min_minutes_above_vwap` (для long), затем коснуться VWAP и закрыться обратно выше VWAP с объёмом > volume_min_ratio × avg → вход по тренду.  
4. Стоп ATR, RR из params, до EOD 18:40.

**Не** fade от SMA (отклонённый mean_reversion).

### Полный universe MGNT/ROSN/TATN (frequency wave1) — отклонено

| Run | exp_R | PF | WF | trades | PnL | Вердикт |
|-----|------:|---:|---:|-------:|----:|---------|
| wave1 seed1 | +0,16 | 1,27 | 13/23 | 119 | +19,0k | PF < 1,3; TATN минус |
| wave1 seed2 | +0,10 | 1,17 | 12/23 | 87 | +8,6k | отклонено |

### Whitelist MGNT+ROSN only — GO candidate

| Параметр | Значение (seed1 best) |
|----------|----------------------|
| strategy.type | vwap_pullback_continuation |
| stop_mode | atr |
| atr_period | 14 |
| atr_multiplier | 1,585 |
| reward_ratio | 2,046 |
| volume_filter | true |
| volume_min_ratio | 2,299 |
| breakout_threshold | 0,01167 |
| max_trades_per_ticker_per_day | 3 |
| trail_activation_r | 1,591 |
| trail_stage_max | 2 |
| strategy_entry_delay_minutes | 36 (~10:36) |
| orb_minutes | 16 |
| min_minutes_above_vwap | 49 |
| tickers | MGNT, ROSN |
| risk_per_trade_percent | 0,5 |

| Run | exp_R | PF | WF | trades | PnL | Вердикт |
|-----|------:|---:|---:|-------:|----:|---------|
| wave1-mgnt-rosn seed1 | +0,26 | 1,48 | 15/23 | 67 | +17,6k | ok |
| wave1-mgnt-rosn-seed2 | +0,27 | 1,48 | 15/23 | 41 | +11,2k | **GO candidate** |

**Предупреждение:** пересечение с ORC (утро) и MF (MGNT днём) — нужен priority rule.

---

## 7. Отклонённые линии (сжатый каталог)

Не возвращаться без **новой** оси (другой principle / слот / universe isolation).

| Линия / идея | Ключевой результат | Почему отклонено |
|--------------|-------------------|------------------|
| opening_range (market ORB) | −31k, 4/23 | хуже ORC; шорты мертвы |
| momentum_breakout, 9 тикеров | −12k, exp −0,07R | нет edge |
| momentum_breakout lean SBER/ROSN/NVTK | −18k | live ≠ backtest |
| momentum_breakout GAZP+CHMF | +0,05R | слишком слабо |
| momentum_breakout afternoon GAZP+CHMF | exp −0,02R | нет |
| mean_reversion (SMA fade) matrix | −17k | без whitelist |
| mean_reversion afternoon MGNT/ROSN/TATN | exp −0,01R, −2,6k | нет |
| momentum_filtered без delay+whitelist | −27k | нужен слот+whitelist |
| OR Fade maxEntries↑ | сделки не растут | не рычаг частоты |
| VWAP MGNT/ROSN/TATN | seed2 PF 1,17 | TATN убивает |
| momentum_sber_daytrend | +0,01R, PF 1,01 | Calmar-ловушка |
| midday_compression_breakout (LKOH/MOEX, сжатие ATR → пробой) | −0,23R, 9/23, −10k | обеденная низкая ликвидность ≠ сигнал |
| late_session_imbalance (18:00–18:35, volume surge) | −0,01R, PF 0,98, −2k | узкое окно, EOD режет |
| MF solo SBER/NVTK/ROSN | все минус | matrix не переносится |
| ORC LKOH solo | seed2 PF 1,25 | нестабильно |
| prev_day_level_breakout (пробой yesterday H/L) | −0,10R, −43k, 449 сделок | нет edge |
| afternoon_range_fade (OR Fade-логика на range 12:30–14:00) | −0,13R | нет edge |
| OR Fade re-tune +TATN | 22–24 сделки | хуже fixed +TATN |

---

## 8. Per-ticker matrix ORC / OR Fade (2026-07-11) — справочно

### ORC утро

| Тикер | exp_R | PF | WF | Вердикт |
|-------|------:|---:|---:|---------|
| MGNT | +0,70 | 2,00 | 11/23 | core |
| ROSN | +0,38 | 1,49 | 14/23 | ok |
| LKOH | +0,33 | 1,37 | 7/23 | мало WF; later seed2 fail |
| NVTK | +0,17 | 1,20 | 16/23 | слабо |
| TATN | +0,16 | 1,17 | 13/23 | в champion |
| CHMF | +0,05 | 1,04 | 10/23 | нет |
| GAZP | −0,06 | 0,94 | 12/23 | нет |
| MOEX | −0,27 | 0,74 | 7/23 | нет |
| SBER | −0,42 | 0,63 | 9/23 | нет |

### OR Fade утро–полдень

| Тикер | exp_R | PF | WF | trades | Вердикт |
|-------|------:|---:|---:|-------:|---------|
| CHMF | +1,11 | 4,05 | 11/23 | 9 | мало сделок |
| TATN | +0,98 | 3,41 | 8/23 | 12 | позже подтверждён solo+fixed |
| LKOH | +0,64 | 2,37 | 13/23 | 39 | core |
| MOEX | +0,30 | 1,42 | 8/23 | 14 | в champion |
| GAZP | +0,23 | 1,35 | 9/23 | 29 | не в FROZEN |
| SBER | +0,12 | 1,17 | 8/23 | 9 | нет |
| MGNT | +0,07 | 1,11 | 10/23 | 52 | уже в ORC/MF |
| ROSN | −0,01 | 0,99 | 16/23 | 54 | нет для fade |
| NVTK | −0,32 | 0,61 | 11/23 | 24 | нет |

---

## 9. Методология испытаний (как читать результаты)

1. **Гипотеза** = principle + time slot + ticker universe (одна ось за прогон — variable isolation).  
2. **Wave1-wide:** широкий search, ~200 trials, seed 1.  
3. **Wave2 / narrow:** сужение вокруг best.  
4. **Wave3:** whitelist (убрать минусовые тикеры) или fixed-params + новый тикер.  
5. **Seed2:** тот же search, другой seed — проверка стабильности.  
6. Решение по **expectancy_r, PF, WF, числу сделок, seed2** — не по одному Calmar.  
7. Risk% **не** увеличивали ради частоты (это только дисперсия на тех же сделках).  
8. FROZEN champions не перетюнивали целиком без явного запроса; candidates — отдельные snapshot’ы.

### Волны naming (примеры)

- wave1-wide — discovery  
- wave2 / wave2-narrow — уточнение  
- wave3-* — whitelist / universe  
- *-seed2 — стабильность  
- ablation / fixed-* — изоляция одного фактора  

---

## 10. Текущий статус и открытые развилки

**FROZEN в paper portfolio (единственный актуальный набор):** ORC (MGNT/ROSN/TATN) + OR Fade (LKOH/CHMF/MOEX/AFKS) + MF (MGNT/TATN).

**Candidates по метрикам optimizer, но сознательно НЕ влиты в portfolio (2026-07-16):**

1. OR Fade + TATN при неизменных params — конфликт TATN с ORC/MF.
2. VWAP Pullback MGNT+ROSN — конфликт с ORC/MF на тех же тикерах.

Research frequency/whitelist **закрыт**. Не делать без новой гипотезы: risk scaling; возврат к таблице §7; перетюнинг FROZEN «на всякий случай».

---

## 11. Краткая карта «идея → параметры → исход»

| Идея | Ключевые параметры / ось | Исход |
|------|--------------------------|-------|
| OR continuation на ретесте | orb=15, atr≈0,84, RR≈1,64, MGNT/ROSN/TATN | FROZEN +0,49R |
| OR false-breakout fade | orb=26, fade_end=106, atr≈1,23, RR≈1,27, LKOH/CHMF/MOEX/AFKS | FROZEN +0,50R |
| OR Fade + TATN без re-tune | те же params + TATN | candidate +0,48R, 103 trades |
| OR Fade maxEntries 1→3 | только лимит входов | без прироста сделок |
| Afternoon momentum+SMA long | lookback=31, delay 12:30+, MGNT/TATN | FROZEN +0,18R |
| MF + SBER/NVTK/ROSN | тот же principle, solo | отклонено 2026-07-16 |
| VWAP pullback по тренду OR | full 3 тикера | отклонено (TATN) |
| VWAP pullback | MGNT+ROSN, minAboveVWAP=49, delay=36 | candidate +0,26R |
| Midday ATR compression | LKOH/MOEX | отклонено |
| Late session volume | 18:00–18:35 | отклонено |
| Prev-day H/L breakout | MGNT/ROSN/LKOH | отклонено |
| Afternoon range fade | fade после 14:00 | отклонено |
| Market ORB / raw momentum / SMA mean-rev | — | отклонено ранее |

---

*Конец отчёта. Срез 2026-07-16. Все цифры — walk-forward backtest TQBR M5 с комиссией 0,008%/leg, депозит 200k, риск 0,5%/сделку.*
