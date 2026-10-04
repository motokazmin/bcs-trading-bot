"""Пары «обыкновенная / привилегированная»: описательный разбор спреда до регистрации гипотезы.

python3 scripts/pairs/describe.py  (данные: data/pairs-d, data/pairs — sync-history -output-dir data/pairs)

Только свойства спреда s = ln(преф / обычка): уровень по годам, скачки на ночных гэпах
(дивидендные отсечки), скорость возврата к среднему, размах отклонений против издержек.
Доходность торговых правил здесь не считается — правило регистрируется после этого разбора.
"""
import os

import numpy as np
import pandas as pd

ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
DAILY = os.path.join(ROOT, "data", "pairs-d")
M5 = os.path.join(ROOT, "data", "pairs")
PAIRS = [("SBER", "SBERP"), ("TATN", "TATNP"), ("SNGS", "SNGSP"),
         ("MTLR", "MTLRP"), ("RTKM", "RTKMP"), ("BANE", "BANEP")]
END = "2026-09-30"
# Издержки пары на круг: 4 ноги × (комиссия 0.8 + проскальзывание 1.0) б.п., в единицах спреда.
COST_BPS = 4 * 1.8
GAP_BPS = 200  # ночной скачок спреда, который считаем событием (дивиденды, новости)


def load(path):
    df = pd.read_csv(path)
    ts = pd.to_datetime(df["timestamp"], utc=True).dt.tz_convert("Europe/Moscow").dt.tz_localize(None)
    df.index = ts
    return df.drop(columns=["timestamp"])


def daily_pair(c, p):
    a, b = load(os.path.join(DAILY, c + ".csv")), load(os.path.join(DAILY, p + ".csv"))
    j = a.join(b, how="inner", lsuffix="_c", rsuffix="_p")
    j = j[(j.index.weekday < 5) & (j.index <= END)]
    j["s"] = np.log(j["close_p"] / j["close_c"])
    # Ночной скачок спреда: гэп префа минус гэп обычки.
    j["gap"] = (np.log(j["open_p"] / j["close_p"].shift(1)) - np.log(j["open_c"] / j["close_c"].shift(1))) * 1e4
    j["intraday"] = (np.log(j["close_p"] / j["open_p"]) - np.log(j["close_c"] / j["open_c"])) * 1e4
    return j


def half_life(x):
    """AR(1) по уровню: x_t − x_{t−1} = a + b·x_{t−1}; полупериод = −ln2 / ln(1+b)."""
    x = x.dropna()
    dx, lag = x.diff().iloc[1:], x.shift(1).iloc[1:]
    b = np.polyfit(lag.values, dx.values, 1)[0]
    return -np.log(2) / np.log1p(b) if -1 < b < 0 else np.inf


def m5_pair(c, p):
    a, b = load(os.path.join(M5, c + ".csv")), load(os.path.join(M5, p + ".csv"))
    j = a.join(b, how="inner", lsuffix="_c", rsuffix="_p")
    t = j.index
    main = (t.weekday < 5) & (t.hour >= 10) & ((t.hour < 18) | ((t.hour == 18) & (t.minute < 40)))
    j = j[main & (t <= END + " 23:59")]
    j["s"] = np.log(j["close_p"] / j["close_c"]) * 1e4
    j["day"] = j.index.normalize()
    return j


def report_daily(c, p):
    j = daily_pair(c, p)
    print(f"\n=== {c}/{p}, дневки {j.index[0].date()} → {j.index[-1].date()}, {len(j)} дней")
    by_year = j.groupby(j.index.year)["s"].agg(["mean", "std", "min", "max"]) * 1e4
    print("уровень спреда ln(преф/обычка), б.п.:")
    print(by_year.round(0).astype(int).to_string())
    ev = j[j["gap"].abs() >= GAP_BPS]
    print(f"ночных скачков спреда ≥ {GAP_BPS} б.п.: {len(ev)}")
    if len(ev):
        top = ev.reindex(ev["gap"].abs().sort_values(ascending=False).index).head(12)
        print(top[["gap"]].round(0).astype(int).T.to_string())
    s = j["s"] * 1e4
    clean = s.diff().where(j["gap"].abs() < GAP_BPS)  # без ночных событий
    print(f"дневное изменение спреда без событий: std {clean.std():.0f} б.п., "
          f"автокорр. лаг1 {clean.autocorr(1):+.2f}, лаг5 {clean.autocorr(5):+.2f}")
    hl = {y: half_life(g["s"]) for y, g in j.groupby(j.index.year)}
    print("полупериод AR(1) уровня по годам, дней:", {y: (round(v) if np.isfinite(v) else "∞") for y, v in hl.items()})


def report_m5(c, p):
    j = m5_pair(c, p)
    print(f"--- M5 {j.index[0].date()} → {j.index[-1].date()}, {j['day'].nunique()} дней")
    turn = j.groupby("day")[["volume_c", "volume_p"]].sum().median() / 1e6
    print(f"оборот, медиана млн ₽/день: обычка {turn['volume_c']:.0f}, преф {turn['volume_p']:.0f}")
    # Отклонение от среднего за прошлые 104 бара (~день основной сессии), только прошлое.
    dev = j["s"] - j["s"].rolling(104).mean().shift(1)
    ds = j.groupby("day")["s"].diff()  # внутри дня, без ночных переходов
    print(f"изменение спреда за бар: std {ds.std():.1f} б.п.; автокорр. лаг1 {ds.autocorr(1):+.2f}, "
          f"лаг2 {ds.autocorr(2):+.2f}, лаг12 {ds.autocorr(12):+.2f}")
    q = dev.abs().quantile([0.5, 0.9, 0.99])
    print(f"|отклонение от среднего 104 баров|, б.п.: медиана {q[0.5]:.0f}, p90 {q[0.9]:.0f}, p99 {q[0.99]:.0f}; "
          f"издержки круга {COST_BPS:.1f}")
    hl = {}
    for y, g in j.groupby(j.index.year):
        x = (g["s"] - g["s"].rolling(104).mean().shift(1))
        hl[y] = half_life(x)
    print("полупериод отклонения по годам, баров M5:", {y: (round(v) if np.isfinite(v) else "∞") for y, v in hl.items()})
    # Пустые бары: close не менялся — бумага не торговалась или торговалась по одной цене.
    flat = (j[["close_c", "close_p"]].diff() == 0).mean() * 100
    print(f"баров без изменения close, %: обычка {flat['close_c']:.0f}, преф {flat['close_p']:.0f}")


def main():
    for c, p in PAIRS:
        report_daily(c, p)
        report_m5(c, p)


if __name__ == "__main__":
    main()
