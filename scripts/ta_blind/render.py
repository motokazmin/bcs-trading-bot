"""Картинка для решения: только прошлое, без тикера и дат, цена нормирована (последний close = 100).

Запуск: python3 scripts/ta_blind/render.py p000 [p001 …] → results/ta-blind/charts/<id>.png
"""
import os
import sys

import matplotlib

matplotlib.use("Agg")
import matplotlib.pyplot as plt
import numpy as np
import pandas as pd

from common import WORK, load_points, past

WD = ["пн", "вт", "ср", "чт", "пт", "сб", "вс"]


def candles(ax, df, up="#2e8b57", down="#c0392b", w=0.6):
    x = np.arange(len(df))
    o, h, l, c = (df[k].to_numpy() for k in ("open", "high", "low", "close"))
    col = np.where(c >= o, up, down)
    ax.vlines(x, l, h, color=col, linewidth=0.8)
    body_lo, body_hi = np.minimum(o, c), np.maximum(o, c)
    ax.bar(x, np.maximum(body_hi - body_lo, 1e-6), bottom=body_lo, width=w, color=col, edgecolor=col, linewidth=0.3)
    ax.set_xlim(-1, len(df))
    ax.grid(alpha=0.25)
    return x


def agg(df, key):
    g = df.groupby(key, sort=True)
    return pd.DataFrame({"open": g["open"].first(), "high": g["high"].max(), "low": g["low"].min(),
                         "close": g["close"].last(), "volume": g["volume"].sum(), "ts": g["ts"].first()}).reset_index(drop=True)


def atr(d, n=14):
    pc = d["close"].shift()
    tr = pd.concat([d["high"] - d["low"], (d["high"] - pc).abs(), (d["low"] - pc).abs()], axis=1).max(axis=1)
    return tr.rolling(n).mean()


def render(p):
    t = pd.Timestamp(p["t"])
    df = past(p["ticker"], t).copy()
    k = 100.0 / df["close"].iloc[-1]
    for c in ("open", "high", "low", "close"):
        df[c] = df[c] * k
    df["day"] = df["ts"].dt.date

    daily = agg(df, "day").tail(60)
    df["hour"] = df["ts"].dt.floor("h")
    hourly = agg(df, "hour").tail(48)

    days = sorted(df["day"].unique())
    today, prev = days[-1], days[-2]
    pv = df[df["day"] == prev]
    td = df[df["day"] == today]
    m5 = df[df["day"].isin([prev, today])].reset_index(drop=True)
    m5["ema20"] = df["close"].ewm(span=20, adjust=False).mean().iloc[-len(m5):].values
    tp = (td["high"] + td["low"] + td["close"]) / 3
    vwap = (tp * td["volume"]).cumsum() / td["volume"].cumsum().replace(0, np.nan)

    fig = plt.figure(figsize=(15, 10), dpi=80)
    gs = fig.add_gridspec(3, 2, height_ratios=[1, 2.2, 0.5], hspace=0.25, wspace=0.12)

    ax1 = fig.add_subplot(gs[0, 0])
    candles(ax1, daily)
    ax1.set_title("D1, 60 сессий (последняя — сегодня, неполная)", fontsize=10)
    ax1.set_xticks([])

    ax2 = fig.add_subplot(gs[0, 1])
    x2 = candles(ax2, hourly)
    hd = hourly["ts"].dt.date
    for i in range(1, len(hourly)):
        if hd.iloc[i] != hd.iloc[i - 1]:
            ax2.axvline(i - 0.5, color="#999", lw=0.6, ls=":")
    ax2.set_title("H1, последние ~48 часов торгов (пунктир — смена дня)", fontsize=10)
    ax2.set_xticks([])

    ax3 = fig.add_subplot(gs[1, :])
    x3 = candles(ax3, m5, w=0.7)
    ax3.plot(x3, m5["ema20"], color="#1f77b4", lw=1.1, label="EMA20 (M5)")
    tdi = m5.index[m5["day"] == today]
    ax3.plot(tdi, vwap.values, color="#8e44ad", lw=1.3, label="VWAP сегодня")
    split = tdi.min()
    ax3.axvline(split - 0.5, color="#555", lw=0.8, ls="--")
    for lvl, name, col in ((pv["high"].max(), "вчера high", "#2e8b57"), (pv["low"].min(), "вчера low", "#c0392b"),
                           (pv["close"].iloc[-1], "вчера close", "#555")):
        ax3.axhline(lvl, color=col, lw=0.8, ls=":")
        ax3.text(0, lvl, f"{name} {lvl:.2f} ", va="bottom", fontsize=8, color=col)
    hours = m5.index[m5["ts"].dt.minute == 0]
    ax3.set_xticks(hours)
    ax3.set_xticklabels(m5["ts"].dt.strftime("%H:%M").loc[hours], fontsize=8)
    main_open = m5.index[(m5["day"] == today) & (m5["ts"].dt.strftime("%H:%M") == "10:00")]
    if len(main_open):
        ax3.axvline(main_open[0] - 0.5, color="#d35400", lw=0.9, ls="-.")
        ax3.text(main_open[0], ax3.get_ylim()[1], " 10:00 основная сессия", va="top", fontsize=8, color="#d35400")
    ax3.set_title("M5: вчера | сегодня (время МСК). Последний бар закрыт — решение на его close = 100.00", fontsize=10)
    ax3.legend(loc="lower right", fontsize=8)
    ax3.yaxis.tick_right()

    ax4 = fig.add_subplot(gs[2, :], sharex=ax3)
    ax4.bar(x3, m5["volume"], color=np.where(m5["close"] >= m5["open"], "#2e8b57", "#c0392b"), width=0.7)
    ax4.set_yticks([])
    ax4.grid(alpha=0.25)

    d_atr = atr(daily).iloc[-2]  # по завершённым дням
    m_atr = atr(m5).iloc[-1]
    gap = (td["open"].iloc[0] / pv["close"].iloc[-1] - 1) * 100
    end = t.strftime("%H:%M")
    left = (pd.Timestamp(f"{t.date()} 18:40", tz=t.tz) - t).total_seconds() / 60
    fig.suptitle(f"{p['id']}   {WD[t.weekday()]} {end} МСК, до выхода по времени (18:40) {left:.0f} мин   |   "
                 f"ATR14 D1 {d_atr:.2f}%   ATR14 M5 {m_atr:.3f}%   гэп дня {gap:+.2f}%   "
                 f"диапазон дня {td['high'].max() - td['low'].min():.2f}%", fontsize=11)
    out = os.path.join(WORK, "charts", p["id"] + ".png")
    os.makedirs(os.path.dirname(out), exist_ok=True)
    fig.savefig(out, bbox_inches="tight")
    plt.close(fig)
    return out


def main():
    pts = load_points()
    for pid in sys.argv[1:]:
        print(render(pts[pid]))


if __name__ == "__main__":
    main()
