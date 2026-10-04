"""0018: книжные индикаторы на дневках — несут ли сигналы информацию о будущем ходе.

python3 scripts/ta_blind/indicators_daily.py  (данные: data/history-d, sync-history -timeframe D)

Сигнал — на close дня t, событие — день, когда условие стало истинным (накануне было ложным).
Доходность open(t+1) → close(t+h) со знаком сигнала минус средняя доходность того же тикера
на том же горизонте по всем дням. SE кластеризована по календарному месяцу.
"""
import glob
import math
import os

import numpy as np
import pandas as pd

ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
HIST = os.path.join(ROOT, "data", "history-d")
END = "2026-09-30"
SPLIT = "2020-10-01"
HORIZONS = (1, 5, 20)
T_CRIT = 3.0
TRADE_BPS = 10.0


def load(path):
    df = pd.read_csv(path)
    ts = pd.to_datetime(df["timestamp"], utc=True).dt.tz_convert("Europe/Moscow")
    df["date"] = ts.dt.tz_localize(None).dt.normalize()
    df = df[(df["date"].dt.weekday < 5) & (df["date"] <= END)]
    return df.drop(columns=["timestamp"]).sort_values("date").reset_index(drop=True)


def rsi(close, n=14):
    d = close.diff()
    up = d.clip(lower=0).ewm(alpha=1 / n, adjust=False).mean()
    dn = (-d.clip(upper=0)).ewm(alpha=1 / n, adjust=False).mean()
    return 100 - 100 / (1 + up / dn)


def onset(cond):
    """Событие — день, когда условие стало истинным."""
    cond = cond.fillna(False).astype(bool)
    return cond & ~cond.shift(1, fill_value=False)


def signals(df):
    o, h, l, c = df["open"], df["high"], df["low"], df["close"]
    r = rsi(c)
    ema12, ema26 = c.ewm(span=12, adjust=False).mean(), c.ewm(span=26, adjust=False).mean()
    macd = ema12 - ema26
    sig = macd.ewm(span=9, adjust=False).mean()
    sma20, sma50, sma200 = c.rolling(20).mean(), c.rolling(50).mean(), c.rolling(200).mean()
    sd20 = c.rolling(20).std(ddof=0)
    hi20, lo20 = h.shift(1).rolling(20).max(), l.shift(1).rolling(20).min()
    red, green = c < o, c > o
    red3 = red.shift(1) & red.shift(2) & red.shift(3)
    green3 = green.shift(1) & green.shift(2) & green.shift(3)
    bull_eng = green & red.shift(1) & (o <= c.shift(1)) & (c >= o.shift(1))
    bear_eng = red & green.shift(1) & (o >= c.shift(1)) & (c <= o.shift(1))
    ready = sma200.notna()  # все индикаторы определены
    out = {
        ("1 RSI 30/70", 1): onset(r > 30) & (r.shift(1) <= 30),
        ("1 RSI 30/70", -1): onset(r < 70) & (r.shift(1) >= 70),
        ("2 MACD×signal", 1): onset(macd > sig),
        ("2 MACD×signal", -1): onset(macd < sig),
        ("3 SMA50×SMA200", 1): onset(sma50 > sma200),
        ("3 SMA50×SMA200", -1): onset(sma50 < sma200),
        ("4 close×SMA200", 1): onset(c > sma200),
        ("4 close×SMA200", -1): onset(c < sma200),
        ("5 Дончиан 20", 1): onset(c > hi20),
        ("5 Дончиан 20", -1): onset(c < lo20),
        ("6 Боллинджер 20/2", 1): onset(c < sma20 - 2 * sd20),
        ("6 Боллинджер 20/2", -1): onset(c > sma20 + 2 * sd20),
        ("7 поглощение", 1): (bull_eng & red3.fillna(False)),
        ("7 поглощение", -1): (bear_eng & green3.fillna(False)),
    }
    # Первые 200 дней индикаторы не определены: их «пересечения» — артефакт разгона.
    return {k: v & ready & ready.shift(1, fill_value=False) for k, v in out.items()}


def clustered(x, clusters):
    x = np.asarray(x, float)
    n = len(x)
    if n < 2:
        return float("nan"), float("nan")
    m = x.mean()
    s = pd.Series(x - m).groupby(np.asarray(clusters)).sum()
    se = math.sqrt((s ** 2).sum()) / n
    return m, se


def main():
    files = sorted(glob.glob(os.path.join(HIST, "*.csv")))
    events = []
    jumps = []
    for f in files:
        tk = os.path.basename(f)[:-4]
        df = load(f)
        rets = df["close"].pct_change().abs()
        jumps += [(tk, d.date(), round(v * 100, 1)) for d, v in zip(df["date"], rets) if v > 0.4]
        sigs = signals(df)
        for hz in HORIZONS:
            fwd = df["close"].shift(-hz) / df["open"].shift(-1) - 1  # open(t+1) → close(t+h)
            base = fwd.mean()
            for (name, side), mask in sigs.items():
                idx = mask & fwd.notna()
                ex = side * (fwd[idx] - base) * 1e4
                for d, v in zip(df.loc[idx, "date"], ex):
                    events.append((name, side, hz, tk, d, v))
    ev = pd.DataFrame(events, columns=["signal", "side", "h", "ticker", "date", "bps"])
    ev["month"] = ev["date"].dt.to_period("M")
    print(f"тикеров {len(files)}, дней {ev['date'].min().date()} → {ev['date'].max().date()}")
    if jumps:
        print(f"дневных ходов > 40% (сплиты/ошибки данных?): {jumps}")

    rows = []
    for (name, side, hz), g in ev.groupby(["signal", "side", "h"]):
        m, se = clustered(g["bps"], g["month"])
        h1 = g[g["date"] < SPLIT]["bps"].mean()
        h2 = g[g["date"] >= SPLIT]["bps"].mean()
        t = m / se if se > 0 else float("nan")
        info = abs(t) >= T_CRIT and np.sign(h1) == np.sign(h2) == np.sign(m)
        rows.append({"сигнал": name, "сторона": "лонг" if side > 0 else "шорт", "h": hz, "n": len(g),
                     "б.п.": m, "t": t, "1-я пол.": h1, "2-я пол.": h2,
                     "информация": "ДА" if info else "", "торгуем": "ДА" if info and m > TRADE_BPS else ""})
    out = pd.DataFrame(rows)
    pd.set_option("display.width", 200)
    print(out.to_string(index=False, float_format=lambda v: f"{v:+.1f}"))
    out.to_csv(os.path.join(ROOT, "results", "indicators-daily.csv"), index=False)
    print(f"\nпрошли критерий информации: {(out['информация'] == 'ДА').sum()} из {len(out)}; "
          f"торгуемых: {(out['торгуем'] == 'ДА').sum()}")


if __name__ == "__main__":
    main()
