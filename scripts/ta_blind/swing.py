"""Свинг по дневным графикам (docs/analysis/0017): точки, картинка, исход.

TA_BLIND_DIR=results/ta-blind-swing python3 swing.py sample 200 2027
TA_BLIND_DIR=results/ta-blind-swing python3 swing.py render p000 p001 …
TA_BLIND_DIR=results/ta-blind-swing python3 swing.py settle [--report]
Решения пишутся тем же decide.py.

Момент решения — конец бара 18:35 (18:40 МСК, конец основной сессии). Вход — open следующего
M5-бара. Удержание — до close последнего бара, кончающегося не позже 18:40 пятого торгового
дня после входа. Стоп/цель проверяются по всем M5-барам (вечерние сессии и гэпы — тоже).
"""
import json
import math
import os
import random
import sys

import matplotlib

matplotlib.use("Agg")
import matplotlib.pyplot as plt
import numpy as np
import pandas as pd

from common import TICKERS, WORK, journal_path, load, load_journal, load_points, past, points_path
from render import agg, atr, candles, WD
from settle import simulate

FROM, TO = "2025-01-15", "2026-09-20"
HOLD_DAYS = 5


def sample(n, seed):
    rng = random.Random(seed)
    os.makedirs(WORK, exist_ok=True)
    if os.path.exists(points_path()):
        sys.exit("points.json уже есть — точки генерируются один раз")
    cands = {}
    for t in TICKERS:
        df = load(t)
        e = df["end"]
        m = (e.dt.strftime("%H:%M") == "18:40") & (e.dt.weekday < 5) \
            & (e >= pd.Timestamp(FROM, tz=e.dt.tz)) & (e < pd.Timestamp(TO, tz=e.dt.tz))
        cands[t] = list(e[m])
    pts = []
    for i in range(n):
        t = rng.choice(TICKERS)
        pts.append({"id": f"p{i:03d}", "ticker": t, "t": rng.choice(cands[t]).isoformat()})
    with open(points_path(), "w") as f:
        json.dump(pts, f, indent=0)
    print(f"{n} точек → {points_path()}")


def render(p):
    t = pd.Timestamp(p["t"])
    df = past(p["ticker"], t).copy()
    k = 100.0 / df["close"].iloc[-1]
    for c in ("open", "high", "low", "close"):
        df[c] = df[c] * k
    df["day"] = df["ts"].dt.date
    daily = agg(df, "day")
    daily["sma20"] = daily["close"].rolling(20).mean()
    daily["sma50"] = daily["close"].rolling(50).mean()
    d = daily.tail(120).reset_index(drop=True)
    df["hour"] = df["ts"].dt.floor("h")
    hourly = agg(df, "hour").tail(90).reset_index(drop=True)

    fig = plt.figure(figsize=(15, 10), dpi=80)
    gs = fig.add_gridspec(3, 1, height_ratios=[2.4, 0.5, 1.4], hspace=0.2)
    ax1 = fig.add_subplot(gs[0])
    x = candles(ax1, d)
    ax1.plot(x, d["sma20"], color="#1f77b4", lw=1.1, label="SMA20")
    ax1.plot(x, d["sma50"], color="#e67e22", lw=1.1, label="SMA50")
    ax1.legend(loc="upper left", fontsize=8)
    ax1.yaxis.tick_right()
    ax1.set_xticks(x[::10])
    ax1.set_xticklabels([f"-{len(d) - 1 - i}" for i in x[::10]], fontsize=8)
    ax1.set_title("D1, 120 сессий (подписи — сессий назад; последняя — сегодня, закрыта). Решение на её close = 100.00", fontsize=10)
    ax2 = fig.add_subplot(gs[1], sharex=ax1)
    ax2.bar(x, d["volume"], color=np.where(d["close"] >= d["open"], "#2e8b57", "#c0392b"), width=0.6)
    ax2.set_yticks([])
    ax3 = fig.add_subplot(gs[2])
    x3 = candles(ax3, hourly)
    hd = hourly["ts"].dt.date
    for i in range(1, len(hourly)):
        if hd.iloc[i] != hd.iloc[i - 1]:
            ax3.axvline(i - 0.5, color="#999", lw=0.6, ls=":")
    ax3.yaxis.tick_right()
    ax3.set_xticks([])
    ax3.set_title("H1, последние ~90 часов торгов (пунктир — смена дня)", fontsize=10)
    a = atr(daily).iloc[-1]
    ch20 = (d["close"].iloc[-1] / d["close"].iloc[-21] - 1) * 100
    fig.suptitle(f"{p['id']}   {WD[t.weekday()]}, конец основной сессии   |   ATR14 D1 {a:.2f}%   "
                 f"изм. за 20 сессий {ch20:+.1f}%   удержание до 5 торговых дней", fontsize=11)
    out = os.path.join(WORK, "charts", p["id"] + ".png")
    os.makedirs(os.path.dirname(out), exist_ok=True)
    fig.savefig(out, bbox_inches="tight")
    plt.close(fig)
    return out


def settle(report):
    pts, jr = load_points(), load_journal()
    rows = []
    for pid, d in sorted(jr.items()):
        if d["action"] not in ("long", "short"):
            continue
        p = pts[pid]
        t = pd.Timestamp(p["t"])
        df = load(p["ticker"])
        fut = df[df["ts"] >= t]
        days = sorted(fut["ts"].dt.date.unique())
        # день входа = первый день будущих баров; выход — 18:40 пятого торгового дня (будни)
        wd = [x for x in days if pd.Timestamp(x).weekday() < 5]
        last = wd[min(HOLD_DAYS, len(wd) - 1)]
        eod = pd.Timestamp(f"{last} 18:40", tz=t.tz)
        fut = fut[fut["end"] <= eod]
        side = 1 if d["action"] == "long" else -1
        stop_d = abs(100 - d["stop"]) / 100
        tgt_d = abs(d["target"] - 100) / 100
        r, why = simulate(fut, side, fut["open"].iloc[0], stop_d, tgt_d)
        rm, _ = simulate(fut, -side, fut["open"].iloc[0], stop_d, tgt_d)
        rows.append({"id": pid, "action": d["action"], "setup": d.get("setup", ""), "r": r, "why": why,
                     "mirror_r": rm, "stop_bps": stop_d * 1e4, "rr": tgt_d / stop_d})
    out = pd.DataFrame(rows)
    out.to_csv(os.path.join(WORK, "settled.csv"), index=False)
    n = len(out)
    print(f"решений {len(jr)}, сделок {n}, пропусков {len(jr) - n}")
    if n < 2:
        return
    se = out["r"].std(ddof=1) / math.sqrt(n)
    diff = out["r"] - out["mirror_r"]
    se_d = diff.std(ddof=1) / math.sqrt(n)
    print(f"exp_R {out['r'].mean():+.3f} ± {se:.3f} (SE)   win {100*(out['r']>0).mean():.1f}%   sum {out['r'].sum():+.1f}R")
    print(f"зеркало exp_R {out['mirror_r'].mean():+.3f}   разница {diff.mean():+.3f} ± {se_d:.3f} (t = {diff.mean()/se_d:+.2f})")
    print(f"стоп медиана {out['stop_bps'].median():.0f} б.п., RR медиана {out['rr'].median():.2f}")
    print("выходы:", out["why"].value_counts().to_dict())
    if report:
        print(out.groupby("setup")[["r", "mirror_r"]].agg(["count", "mean"]).round(3).to_string())
        print(out.groupby("action")[["r", "mirror_r"]].mean().round(3).to_string())


if __name__ == "__main__":
    cmd = sys.argv[1]
    if cmd == "sample":
        sample(int(sys.argv[2]), int(sys.argv[3]))
    elif cmd == "render":
        pts = load_points()
        for pid in sys.argv[2:]:
            print(render(pts[pid]))
    elif cmd == "settle":
        settle("--report" in sys.argv)
