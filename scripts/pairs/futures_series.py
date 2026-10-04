"""Непрерывные ряды фьючерсов на акции из контрактов ISS (data/futures-d) → data/futures-d/continuous/.

python3 scripts/pairs/futures_series.py

Держим ближайший контракт; за ROLL_DAYS торговых дней до его последнего дня торгов — следующий.
Доходность дня считается внутри одного контракта (расчётная цена к расчётной вчера), поэтому
стык контрактов не даёт скачка. День перекладки помечен `roll` — на нём платятся издержки двух ног.
"""
import glob
import os

import numpy as np
import pandas as pd

ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
SRC = os.path.join(ROOT, "data", "futures-d")
OUT = os.path.join(SRC, "continuous")
ROLL_DAYS = 5
# Переименования базового актива: один эмитент — один ряд.
ALIAS = {"YNDF": "YDEX", "TCSI": "T", "FIVE": "X5", "CHMFM": "CHMF", "PLZLM": "PLZL", "NOTKM": "NOTK"}
END = pd.Timestamp(os.environ.get("PAIRS_END", "2026-09-30"))  # см. distance.END


def load_contracts():
    frames = []
    for p in glob.glob(os.path.join(SRC, "*.csv")):
        if os.path.basename(p) == "contracts.csv":
            continue
        df = pd.read_csv(p)
        if df.empty:
            continue
        frames.append(df)
    df = pd.concat(frames, ignore_index=True)
    df["TRADEDATE"] = pd.to_datetime(df["TRADEDATE"])
    # у части старых контрактов ISS не заполняет ASSETCODE — базовый актив из названия (GAZR-3.17)
    df["asset"] = df["ASSETCODE"].fillna(df["SHORTNAME"].str.split("-").str[0]).replace(ALIAS)
    df["SECID"] = df["SHORTNAME"]  # код повторяется раз в 10 лет — контракт определяет название
    df = df[(df["SETTLEPRICE"] > 0) & (df["TRADEDATE"] <= END)]
    return df


def expiries(df):
    """Последний день торгов контракта. У ещё торгующихся на END — из названия (SBRF-12.26 → 2026-12-15):
    иначе «экспирацией» стал бы конец данных и перекладка ушла бы раньше времени."""
    last = df.groupby("SECID")["TRADEDATE"].max()
    out = {}
    for s, d in last.items():
        if d >= END - pd.Timedelta(days=7):
            m, y = s.rsplit("-", 1)[1].split(".")
            d = max(d, pd.Timestamp(2000 + int(y), int(m), 15))
        out[s] = d
    return pd.Series(out)


def build(asset, df):
    expiry = expiries(df)
    days = np.sort(df["TRADEDATE"].unique())
    pos = {d: i for i, d in enumerate(days)}
    by = {s: g.set_index("TRADEDATE") for s, g in df.groupby("SECID")}
    rows, held = [], None
    for d in days:
        # Кандидаты: торгуются сегодня и до экспирации больше ROLL_DAYS торговых дней.
        live = [s for s, e in expiry.items() if d in by[s].index and e > d
                and (pos.get(e, 10**9) - pos[d]) > ROLL_DAYS]  # экспирация за концом данных — далеко
        if not live:
            continue
        nearest = min(live, key=lambda s: expiry[s])
        roll = held is not None and nearest != held
        ret = np.nan
        if held is not None:
            # доходность дня — по контракту, который держали вчера, от его расчётной цены вчера
            y, prev_day = by[held], rows[-1]["date"]
            if d in y.index and prev_day in y.index:
                ret = y.loc[d, "SETTLEPRICE"] / y.loc[prev_day, "SETTLEPRICE"] - 1
        cur = by[nearest].loc[d]
        rows.append({"date": d, "secid": nearest, "settle": cur["SETTLEPRICE"], "ret": ret, "roll": roll,
                     "value": cur["VALUE"], "oi": cur["OPENPOSITION"]})
        held = nearest
    return pd.DataFrame(rows)


def main():
    os.makedirs(OUT, exist_ok=True)
    df = load_contracts()
    print(f"{'актив':6} {'с':10} {'по':10} {'дней':>5} {'перекл.':>7}  медиана оборота млн ₽/день: 2015-19 / 2020-22 / 2023-26")
    for asset, g in sorted(df.groupby("asset")):
        s = build(asset, g)
        if s.empty:
            continue
        s.to_csv(os.path.join(OUT, asset + ".csv"), index=False)
        y = s["date"].dt.year
        med = [s.loc[(y >= a) & (y <= b), "value"].median() / 1e6 for a, b in ((2015, 2019), (2020, 2022), (2023, 2026))]
        med = " / ".join(f"{m:7.0f}" if np.isfinite(m) else "      —" for m in med)
        print(f"{asset:6} {s['date'].iloc[0].date()} {s['date'].iloc[-1].date()} {len(s):5d} {int(s['roll'].sum()):7d}  {med}")


if __name__ == "__main__":
    main()
