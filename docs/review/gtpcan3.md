Поправки принял. В частности, live `trailOnBar → close` больше не считаю самостоятельным источником смещения baseline; это вопрос выравнивания двух реализаций, а не качества стратегии. И согласен: при отсутствии нетронутого временного хвоста нельзя называть повторный прогон 2026-04→09 честным OOS.

Я бы сейчас не строил ещё один ORB/ранний breakout. В коде уже видно, что текущие конструкции в основном ищут одностороннее движение относительно собственного исторического диапазона: ORC использует breakout close и уровень диапазона, а `MomentumFiltered` — breakout относительно `rangeLevels()`; последний вообще получает свечи только после session-delay и сохраняет буфер между днями.  

Из ваших результатов я бы сделал другой вывод: **если базовая внутридневная инерция действительно около +0.037R, искать стоит не "ещё более точный breakout", а способ отобрать ситуации, в которых эта инерция либо усиливается потоком, либо возникает относительно других бумаг.** Ниже — четыре независимые гипотезы.

---

# 1. Morning Gap Continuation

### Механизм

После большой overnight-gap изменение цены уже произошло до нашего решения. Это не ORB: диапазон сегодняшнего утра вообще не участвует в сигнале.

Моя гипотеза: часть overnight price discovery приходит не одним потоком. Первые 1–2 M5-бара после 07:00 продолжают перерабатывать информацию/инвентарь, поэтому **большой gap + подтверждение первым баром** может иметь продолжение.

Кто вынужден торговать: overnight inventory, участники утреннего аукциона/первого окна ликвидности и исполнители крупных ордеров. **Предполагаю**, что на MOEX это даёт остаточную инерцию сильнее случайного входа.

### Точные правила

Вселенная: **10 боевых бумаг**.

Только утренняя сессия.

`PrevClose` = последний доступный close перед сегодняшним 07:00. Это может быть close вечерней сессии предыдущего дня; не используем никакие сегодняшние значения до начала первого бара.

На первом M5:

`gap = (Open_07:00 / PrevClose - 1)`

Сигнал на close 07:05:

**BUY**, если:

* `gap >= +40 б.п.`;
* `Close_07:05 - PrevClose >= 0.50 × gap`;
* иначе no trade.

**SELL** симметрично.

Вход: **07:05 close**.

Выход:

* SL = `0.50 × |gap|` от entry;
* TP = `0.75 × |gap|` от entry;
* максимум 6 баров;
* если ни SL, ни TP не сработали — close шестого бара.

На одну бумагу максимум одна такая сделка утром.

Свободный параметр здесь только один: `gap_min`. Для первой проверки зафиксировать 40 б.п. Остальное не оптимизировать.

Почему именно такой каркас: при минимальном gap 40 б.п. стоп составляет 20 б.п., то есть круговые 3.6 б.п. — примерно `0.18R`; TP при 75% gap — минимум около 30 б.п., то есть уже в ~8.3 раза больше комиссии. **Предполагаю**, что это достаточно широкий сигнал, чтобы не превратить стратегию в торговлю спредом.

### Ожидаемый масштаб

На 10 тикерах и примерно 27 месяцах это около **5–6 тыс. утренних возможностей** — по одной на ticker/day.

При частоте сигнала 8–12% это порядок **400–700 сделок**. Это **предположение**, не измерение: реальный count должен дать pandas-скрипт.

Если после условия gap ≥40 б.п. остаётся **<200 сделок**, эту конкретную конструкцию я бы не разрабатывал дальше: выборка слишком мала.

### Дешёвый pandas-тест

```python
import pandas as pd
import numpy as np
from pathlib import Path

TICKERS = {"SBER","GAZP","LKOH","ROSN","NVTK","TATN","MGNT","CHMF","MOEX","AFKS"}
GAP_MIN = 0.0040

def load_history(path):
    df = pd.read_csv(path, parse_dates=["timestamp"])
    df["timestamp"] = pd.to_datetime(df["timestamp"], utc=True)
    return df.sort_values("timestamp")

def screen(df):
    df = df[df.ticker.isin(TICKERS)].copy()
    df["date"] = df.timestamp.dt.tz_convert("Europe/Moscow").dt.date
    local = df.timestamp.dt.tz_convert("Europe/Moscow")
    df["hhmm"] = local.dt.strftime("%H:%M")

    # Последний close до утреннего открытия.
    prev = (
        df[df["hhmm"] < "07:00"]
        .groupby(["ticker", "date"])["close"]
        .last()
        .rename("prev_close")
    )

    m = df[df["hhmm"] == "07:00"].copy()
    m = m.join(prev, on=["ticker", "date"])
    m["gap"] = m["open"] / m["prev_close"] - 1

    first = df[df["hhmm"] == "07:00"][
        ["ticker","date","open","high","low","close"]
    ]
    first = first.join(prev, on=["ticker","date"])
    first["gap"] = first["open"] / first["prev_close"] - 1
    first["confirmed"] = (
        (np.sign(first["close"] - first["prev_close"]) == np.sign(first["gap"])) &
        ((first["close"] - first["prev_close"]).abs() >= 0.5 * first["gap"].abs()) &
        (first["gap"].abs() >= GAP_MIN)
    )

    sig = first[first.confirmed].copy()
    print("signals:", len(sig))
    print("per ticker:")
    print(sig.groupby("ticker").size())

    # Последующие 6 закрытий — грубый first-pass screen.
    bars = df.sort_values(["ticker","timestamp"]).copy()
    bars["entry"] = bars["close"]
    for k in [1,3,6]:
        bars[f"fwd_{k}"] = bars.groupby("ticker")["close"].shift(-k) / bars["entry"] - 1

    sig = sig.merge(
        bars[["ticker","date","close","fwd_1","fwd_3","fwd_6"]],
        on=["ticker","date","close"],
        how="left",
    )
    print(sig[["fwd_1","fwd_3","fwd_6"]].describe())

# Например:
# screen(load_history("data/history/SBER.csv"))
```

Это пока screen, а не backtest: он отвечает на вопрос «после такого gap вообще есть направленное движение?». Если медианный/mean forward return не превышает `3.6 б.п.` с большим запасом, стратегию сразу выбрасываем.

**Не опровергнута стратегия**, если только aggregate результат положительный за счёт одного периода. Для первой проверки я бы требовал одинаковый знак хотя бы в 4 из 6 последовательных временных блоков.

### Чем отличается

Это не ORC: нет диапазона, breakout threshold или ретест-лимита. Нет текущего `OnCandle`-канала.

Главное — **сигнал возникает из overnight displacement**, которого нынешние слоты почти не используют.

### Главный риск самообмана

`PrevClose` легко перепутать с последним close **той же торговой сессии** или фактически использовать сегодняшние вечерние данные. Нужно строить `PrevClose` исключительно из timestamp < сегодняшнего 07:00.

Второй риск — сильные gaps возникают в конкретных новостных режимах, и один исторический эпизод может дать непропорциональную часть PnL.

---

# 2. Cross-Sectional Residual Reversal

Это, на мой взгляд, самая интересная конструкция именно потому, что она почти ортогональна ORC/MF.

### Механизм

Не спрашиваем «SBER вырос выше своего канала?».

Спрашиваем:

> SBER сейчас существенно отклонился от движения **остальных девяти ликвидных бумаг**?

Если общий рынок за 15 минут почти не изменился, а одна бумага резко выбилась относительно остальных, часть движения может быть временным idiosyncratic flow.

Кто ошибается: краткосрочный агрессивный поток в одной акции против общей цены рынка; затем liquidity providers/арбитраж возвращают относительную цену. **Предполагаю**, что именно эта часть order flow на M5 частично mean-reverts.

### Правила

Только `10:00–18:35`.

На каждом M5 close:

`r_i = close_t / close_{t-3} - 1`

`market_r = median(r_i)` по всем 10 бумагам.

`residual_i = r_i - market_r`.

Нормируем residual на cross-sectional MAD текущего `r_i`.

BUY если:

* `z_residual <= -2.5`;
* `|market_r| < 0.40%`.

SELL симметрично.

Вход: close.

Выход:

* SL `0.75 × ATR14`;
* TP `0.75 × ATR14`;
* либо принудительно через 4 M5-бара.

ATR использует только завершённые свечи.

Свободные параметры: фактически только `z=2.5` и `market_move_cap=40 б.п.` на первом этапе. Остальное фиксировано.

### Масштаб

На ~1 млн M5-наблюдений по 10 именам за весь период потенциальная выборка большая. **Предполагаю** порядок **600–2000 сделок после фильтров**, в зависимости от cross-sectional dispersion.

Если `<200`, статистики для дальнейшей оптимизации недостаточно.

### Дешёвый screen

```python
import pandas as pd
import numpy as np

TICKERS = ["SBER","GAZP","LKOH","ROSN","NVTK",
           "TATN","MGNT","CHMF","MOEX","AFKS"]

def residual_screen(df):
    d = df[df.ticker.isin(TICKERS)].copy()
    local = d.timestamp.dt.tz_convert("Europe/Moscow")
    d["hm"] = local.dt.strftime("%H:%M")
    d = d[(d.hm >= "10:00") & (d.hm <= "18:35")]

    d = d.sort_values(["ticker","timestamp"])
    d["r15"] = d.groupby("ticker").close.pct_change(3)

    wide = d.pivot(index="timestamp", columns="ticker", values="r15")
    market = wide.median(axis=1)
    resid = wide.sub(market, axis=0)

    med = resid.median(axis=1)
    mad = resid.sub(med, axis=0).abs().median(axis=1)
    z = resid.sub(med, axis=0).div(mad.replace(0, np.nan), axis=0)

    long_sig = (z <= -2.5) & (market.abs() < 0.004)
    short_sig = (z >= 2.5) & (market.abs() < 0.004)

    print("long observations:", int(long_sig.sum()))
    print("short observations:", int(short_sig.sum()))

    # Смотрим последующие 3/6 баров того же тикера.
    d["fwd3"] = d.groupby("ticker").close.shift(-3) / d.close - 1
    d["fwd6"] = d.groupby("ticker").close.shift(-6) / d.close - 1

    z_long = z.stack().rename("z").reset_index()
    z_long.columns = ["timestamp","ticker","z"]

    x = d.merge(z_long, on=["timestamp","ticker"], how="inner")
    x["side"] = np.where(x.z <= -2.5, 1,
                  np.where(x.z >= 2.5, -1, 0))
    x = x[x.side != 0]

    x["pnl3"] = x.side * x.fwd3
    x["pnl6"] = x.side * x.fwd6

    print(x[["pnl3","pnl6"]].describe())
    print("mean bps 3 bars:", x.pnl3.mean()*1e4)
```

Критерий бросания: если mean residual-reversal move до costs не даёт хотя бы **~8–10 б.п.** при разумной стабильности по времени, я бы не писал Go-реализацию. 3.6 б.п. — математический breakeven, но для нового эффекта нужен запас.

### Отличие

Это не `or-fade`: там нужно, чтобы акция сама пробила OR и вернулась. Здесь **вообще нет opening range**.

Не `mf-afternoon`: нет собственного 39-барного канала, а направление определяется относительно cross-section.

Это также естественно отделяет альфа от общего market beta.

### Главный риск

Cross-sectional MAD сам по себе нельзя рассчитывать из будущих данных — только текущая поперечная выборка допустима на момент close. Нельзя выбирать только те тикеры, где эффект «лучше выглядит».

И очень важный риск — **ticker_busy**. При 94 skips на 1535 сделок (~6%, по вашему измерению) новая стратегия на общем счёте может терять лучшие сигналы из-за другой стратегии. Поэтому сначала screen должен считать **standalone alpha**, а затем отдельно — congestion-adjusted portfolio result.

---

# 3. Volume-Shock Continuation

В текущем `MomentumFiltered` volume filter существует, но чемпион его выключил: `volume_filter: false`. При этом код сравнивает текущий volume с историческим средним.  

Именно поэтому я бы проверил volume не как дополнительный фильтр к старому MF, а как **самостоятельную гипотезу**.

### Механизм

Большой price move + необычно большой объём + закрытие около экстремума бара — кандидат на информационный/aggressive-flow move.

Кто вынужден покупать/продавать: крупный поток, разбитый на последовательные исполнения; небольшие участники, наоборот, склонны фейдить первый импульс. **Предполагаю**, что первые несколько M5 ещё продолжают тот же order-flow.

### Точные правила

Regular session `10:15–18:30`.

Для каждого M5:

1. `ret = Close/Open - 1`.
2. `abs(ret) >= 25 б.п.`
3. `volume >= 1.8 × median(volume)` для **того же времени суток** за предыдущие 20 торговых дней.
4. Close Location Value:

   * BUY: `(Close-Low)/(High-Low) >= 0.80`
   * SELL: `(High-Close)/(High-Low) >= 0.80`.

Вход в направлении бара по close.

SL = `0.60 × ATR14`.

TP = `1.20 × ATR14`.

Максимальное удержание = 4 бара.

То есть здесь всего четыре ручки, но в первом screen **не оптимизировать ни одну**.

Почему same-time volume, а не просто среднее всех прошлых свечей: в текущем `passesVolumeFilter` используется окно истории без коррекции на time-of-day.  **Предполагаю**, что сезонность объёма внутри дня иначе создаст ложные spikes.

### Масштаб

На core-10 **предполагаю** примерно **300–1200 сделок** за 27 месяцев.

Если `<200` — не оптимизировать.

### Дешёвый screen

```python
import pandas as pd
import numpy as np

TICKERS = {"SBER","GAZP","LKOH","ROSN","NVTK",
           "TATN","MGNT","CHMF","MOEX","AFKS"}

def volume_impulse_screen(df):
    d = df[df.ticker.isin(TICKERS)].copy()
    local = d.timestamp.dt.tz_convert("Europe/Moscow")
    d["date"] = local.dt.date
    d["clock"] = local.dt.strftime("%H:%M")

    d = d[(d.clock >= "10:15") & (d.clock <= "18:30")]
    d = d.sort_values(["ticker","timestamp"])

    d["ret"] = d.close / d.open - 1
    d["clv_buy"] = (d.close-d.low) / (d.high-d.low).replace(0,np.nan)
    d["clv_sell"] = (d.high-d.close) / (d.high-d.low).replace(0,np.nan)

    # baseline volume для того же ticker + clock, только ПРЕДЫДУЩИЕ дни
    d["vol_med20"] = (
        d.groupby(["ticker","clock"])["volume"]
         .transform(lambda s: s.shift(1).rolling(20, min_periods=10).median())
    )

    d["atr14"] = (
        d.groupby("ticker")
         .apply(lambda x: np.maximum.reduce([
             x.high-x.low,
             (x.high-x.close.shift(1)).abs(),
             (x.low-x.close.shift(1)).abs()
         ]).rolling(14).mean())
         .reset_index(level=0, drop=True)
    )

    buy = (
        (d.ret >= 0.0025) &
        (d.volume >= 1.8*d.vol_med20) &
        (d.clv_buy >= 0.80)
    )
    sell = (
        (d.ret <= -0.0025) &
        (d.volume >= 1.8*d.vol_med20) &
        (d.clv_sell >= 0.80)
    )

    d["side"] = np.where(buy,1,np.where(sell,-1,0))
    x = d[d.side != 0].copy()

    x["fwd1"] = x.groupby("ticker").close.shift(-1)/x.close - 1
    x["fwd4"] = x.groupby("ticker").close.shift(-4)/x.close - 1

    x["pnl1"] = x.side*x.fwd1
    x["pnl4"] = x.side*x.fwd4

    print("signals:", len(x))
    print("mean next bar, bps:", x.pnl1.mean()*1e4)
    print("mean 4-bar, bps:", x.pnl4.mean()*1e4)
    print(x.groupby("side")[["pnl1","pnl4"]].mean()*1e4)
```

Быстрый kill-rule: если направление next 1–4 bars даёт `≤5 б.п.` даже до затрат, бросить. При 3.6 б.п. round trip это слишком тонкий эффект.

### Отличие

Не ORC: нет диапазона.

Не MF: нет 39-барного breakout и нет long-only.

Не чистый trailing: выход фиксирован TP/SL.

### Самообман

Самая опасная ошибка — volume baseline. Нельзя делать:

`current volume / средний volume за всю историю`.

Иначе 10:00 и 18:00 сравниваются друг с другом. Нужен same-clock historical baseline, как выше.

Вторая ловушка — `High/Low` текущего бара: для самого сигнала они разрешены, потому что решение в close; использовать их в **историческом baseline до current bar** нельзя.

---

# 4. Market Shock → Laggard Catch-Up

Это другая сторона cross-sectional идеи: не фейдить выбившуюся бумагу, а торговать **отложенное присоединение к общему движению**.

### Механизм

Если девять из десяти бумаг одновременно резко двигаются, а одна ликвидная бумага отстаёт, это может быть не независимая информация, а временная скорость распространения общего потока.

Кто вынужден торговать: index/sector/basket-like flows, исполнители крупных заявок, поздно реагирующие участники. **Предполагаю**, что на 5-минутном горизонте такая задержка может быть заметна именно в ликвидных группах.

### Точные правила

Regular `10:15–18:30`.

На close:

`market = median(last 5m return всех 10 тикеров)`.

Сильный market shock:

`|market| >= 30 б.п.`

Для каждого тикера:

`lag = stock_return - market`.

BUY:

* market ≥ +30 б.п.;
* stock lag ≤ −12 б.п.

SELL:

* market ≤ −30 б.п.;
* stock lag ≥ +12 б.п.

Вход в сторону market по close.

SL = `0.60 × ATR14`.

TP = `0.90 × ATR14`.

Time exit = 3 бара.

Максимум одна позиция на ticker.

Свободные параметры: market shock threshold и lag threshold. Стоп/TP/hold фиксированы.

### Масштаб

**Предполагаю 300–1000 сделок** за 27 месяцев на core-10. Если меньше 200, не строить.

### Дешёвый screen

```python
def laggard_screen(df):
    d = df[df.ticker.isin(TICKERS)].copy()
    local = d.timestamp.dt.tz_convert("Europe/Moscow")
    d["hm"] = local.dt.strftime("%H:%M")
    d = d[(d.hm >= "10:15") & (d.hm <= "18:30")]
    d = d.sort_values(["ticker","timestamp"])

    d["r"] = d.groupby("ticker").close.pct_change()

    w = d.pivot(index="timestamp", columns="ticker", values="r")
    market = w.median(axis=1)
    lag = w.sub(market, axis=0)

    buy = (market >= 0.0030) & (lag <= -0.0012)
    sell = (market <= -0.0030) & (lag >= 0.0012)

    sig = buy | sell
    print("BUY candidates:", int(buy.sum()))
    print("SELL candidates:", int(sell.sum()))

    # Forward return по тому же тикеру.
    x = d.copy()
    x["market"] = x.timestamp.map(market)
    x["lag"] = x.timestamp.map(lag.stack())
    x["side"] = 0

    x.loc[
        (x.market >= .003) & (x.lag <= -.0012), "side"
    ] = 1
    x.loc[
        (x.market <= -.003) & (x.lag >= .0012), "side"
    ] = -1

    x["fwd1"] = x.groupby("ticker").close.shift(-1)/x.close - 1
    x["fwd3"] = x.groupby("ticker").close.shift(-3)/x.close - 1

    x = x[x.side != 0]
    x["pnl1"] = x.side*x.fwd1
    x["pnl3"] = x.side*x.fwd3

    print(x[["pnl1","pnl3"]].mean()*1e4)
```

Kill-rule: если средний next/3-bar catch-up `<6 б.п.` до cost или знак ломается на большинстве временных блоков — бросить.

### Отличие

Это не `MomentumFiltered`: MF смотрит на **собственный** предыдущий диапазон:

```go
upper, lower, ok := rangeLevels(s.buffer.history)
...
case close > upper*(1+th):
    direction = "BUY"
```



Здесь сама акция может быть далеко от любого своего high/low. Сигнал возникает только относительно рынка.

---

# Что из четырёх я считаю наиболее интересным

| Гипотеза          | Структурная новизна | Ожидаемый gross move | Ожидаемое N | Главный риск                |
| ----------------- | ------------------- | -------------------: | ----------: | --------------------------- |
| Gap continuation  | высокая             |          20–60+ б.п. |    ~400–700 | regime/news dependence      |
| Residual reversal | очень высокая       |            8–30 б.п. |   ~600–2000 | edge съедается 3.6 б.п.     |
| Volume impulse    | высокая             |           15–40 б.п. |   ~300–1200 | volume seasonality          |
| Laggard catch-up  | очень высокая       |           10–30 б.п. |   ~300–1000 | ложный common-factor signal |

Все численные диапазоны N и типичных движений здесь — **предполагаю**; без самого `data/history` я не могу честно выдать их как измеренные. В приложенном файле есть код и конфиги, но самой истории в доступном вложении нет.

---

# Как честно проверять без нетронутого OOS

Здесь я бы **не делал ещё один «validation хвост» и не называл его OOS**.

Поступил бы так.

### Сначала зафиксировать идеи до просмотра результатов

Прямо сейчас регистрируем:

* эти четыре стратегии;
* exact rules;
* exact universe;
* exact costs;
* exact exits;
* exact thresholds.

После первого screen **никаких новых условий не добавлять**.

### Затем six-block blocked test

27 месяцев разбить на шесть последовательных примерно равных временных блоков.

Для каждой стратегии получить:

`N, gross exp_R, net exp_R, median trade, PF, max DD`

по каждому блоку **отдельно**.

Не выбирать лучший threshold по этим блокам. Threshold уже заморожен.

### Затем ticker cross-check

Так как история содержит 25 тикеров, а боевое ядро — 10, для новых идей особенно полезен второй независимый срез:

* core-10 — основная оценка;
* остальные 15 бумаг — **не для подбора**, а как переносимость гипотезы.

Это особенно важно для стратегий №2 и №4.

### Критерий принятия

Я бы не требовал «общий exp_R > 0» — нулевая модель сама даёт около `+0.037R` по вашим данным.

Для новой стратегии нужен:

`net exp_R > +0.037R`

**и** запас над cost floor, причём не за счёт одного блока.

Практически я бы использовал:

* минимум 200 сделок;
* net `exp_R ≥ +0.08R`;
* не менее 4 из 6 временных блоков положительные;
* ни один блок не хуже примерно `−0.10R`;
* результат при 2 б.п. slippage всё ещё не должен становиться сильно отрицательным.

Последний критерий особенно жёсткий, но у вас портфель уже теряет положительный знак при 2 б.п. — поэтому стратегия, живущая только при 1 б.п., не решает текущую проблему.

### И ещё один контроль

Все четыре стратегии сначала проверять **standalone**.

Только после этого — общий счёт с реальным `ticker_busy`. Ваши ~6% `ticker_busy_skips` показывают, что standalone alpha и реально доступный alpha — уже разные величины. Для портфеля надо считать отдельно:

`standalone exp_R`

и

`exp_R after opportunity blocking`.

Иначе стратегия может оказаться хорошей только на бумаге, потому что лучшие её сделки систематически блокируются другим слотом.

---

# Что проверил бы первым

**№1 — Morning Gap Continuation.**

Причины конкретные.

Во-первых, он даёт **тысячи независимых ticker-day возможностей**, а не десятки сделок вроде `mf-afternoon`.

Во-вторых, ожидаемый размер движения естественно намного больше 3.6 б.п.: gap сам по себе задаёт масштаб. Поэтому меньше риск снова получить стратегию с математическим edge порядка `0.05–0.10R`, который съедается slippage.

В-третьих, это действительно новая информация относительно существующих слотов: текущие ORC/MF ищут продолжение **внутри уже начавшегося intraday price formation**, а gap strategy начинает с overnight displacement.

И главное — её можно **опровергнуть за один вечер без Go, оптимизатора и подгонки**. Если после фиксированного `40 б.п. gap + first-bar confirmation` нет хотя бы порядка 8–10 б.п. направленного следующего движения и устойчивости по временным блокам, эту ветку можно закрыть сразу.

Второй по приоритету для меня — **cross-sectional residual reversal**: он наиболее независим от всех неудачных ORB/breakout конструкций и напрямую использует то, чего сейчас у портфеля почти нет — информацию о том, как одна бумага ведёт себя относительно остальных девяти.
