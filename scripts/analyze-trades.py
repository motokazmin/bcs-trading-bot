#!/usr/bin/env python3
"""Разбор закрытых сделок: качество входа, геометрия стопа, поведение выхода.

Считает то, чего нет в БД: где реально стоял рынок в момент входа, сколько
сделок умерло в свече входа, и что цена делала ПОСЛЕ выхода. Пишет две таблицы:

  data/analysis/trades_enriched.csv — одна строка на сделку + производные метрики
  data/analysis/metrics.csv         — append-only срез по экспериментам на каждый прогон

metrics.csv — журнал прогресса: одна строка = (снимок, эксперимент). Сравнивая
снимки, видно, двигают ли правки метрики в нужную сторону.

Использование:
    python3 scripts/analyze-trades.py [--db data/trades.db] [--history data/history]
                                      [--label "после фикса фила"] [--no-write]
"""
from __future__ import annotations

import argparse
import os
import sqlite3
import subprocess
from datetime import datetime, timezone

import numpy as np
import pandas as pd

BAR = pd.Timedelta(minutes=5)
SESSION_END = pd.Timestamp("1900-01-01 15:40", tz="UTC").time()  # 18:40 MSK
MSK_OFFSET = pd.Timedelta(hours=3)


# --- загрузка ---------------------------------------------------------------

def load_trades(db: str) -> pd.DataFrame:
    with sqlite3.connect(db) as con:
        t = pd.read_sql("SELECT * FROM closed_trades", con)
    if t.empty:
        return t
    t = t.sort_values("opened_at").reset_index(drop=True)
    t["entry_bar_ts"] = pd.to_datetime(t["entry_bar_time"], utc=True, format="ISO8601")
    t["open_utc"] = pd.to_datetime(t["opened_at"]).sub(MSK_OFFSET).dt.tz_localize("UTC")
    t["close_utc"] = pd.to_datetime(t["closed_at"]).sub(MSK_OFFSET).dt.tz_localize("UTC")
    t["sgn"] = np.where(t.direction == "BUY", 1, -1)
    t["level"] = np.where(t.direction == "BUY", t.breakout_upper, t.breakout_lower)
    return t


def load_history(path: str) -> dict[str, pd.DataFrame]:
    out = {}
    for f in sorted(os.listdir(path)):
        if not f.endswith(".csv"):
            continue
        d = pd.read_csv(os.path.join(path, f), parse_dates=["timestamp"])
        out[f[:-4]] = d.set_index("timestamp").sort_index()
    return out


# --- производные метрики ----------------------------------------------------

def enrich(t: pd.DataFrame, H: dict[str, pd.DataFrame]) -> pd.DataFrame:
    """Добавляет диагностику, которую нельзя посчитать без исторических баров."""
    rows = []
    for _, r in t.iterrows():
        d = H.get(r.ticker)
        bar = nxt = None
        if d is not None:
            b = d.loc[d.index == r.entry_bar_ts]
            n = d.loc[d.index == r.entry_bar_ts + BAR]
            bar = b.iloc[0] if len(b) else None
            nxt = n.iloc[0] if len(n) else None

        # Состояние позиции на открытии следующей свечи: <0 — уже против нас.
        pos_at_open = (r.sgn * (nxt.open - r.entry_price) / r.r_distance
                       if nxt is not None and r.r_distance > 0 else np.nan)
        # Где закрылась свеча, в которой записан фил.
        fillbar_close = (r.sgn * (bar.close - r.entry_price) / r.r_distance
                         if bar is not None and r.r_distance > 0 else np.nan)
        # Был ли уровень пробоя реально пройден рынком в свече входа.
        level_touched = (bool(bar.low <= r.level <= bar.high)
                         if bar is not None and r.level > 0 else None)

        # Что цена дала ПОСЛЕ выхода — до конца сессии того же дня.
        mfe_after = np.nan
        if d is not None:
            w = d.loc[(d.index > r.close_utc) & (d.index.date == r.entry_bar_ts.date())]
            w = w[w.index.time <= SESSION_END]
            if len(w) and r.r_distance > 0:
                fav = w.high.max() if r.sgn > 0 else w.low.min()
                mfe_after = (fav - r.entry_price) * r.sgn / r.r_distance

        rows.append((pos_at_open, fillbar_close, level_touched, mfe_after))

    t = t.copy()
    t["pos_at_open_r"] = [x[0] for x in rows]
    t["fillbar_close_r"] = [x[1] for x in rows]
    t["level_touched"] = [x[2] for x in rows]
    t["mfe_after_exit_r"] = [x[3] for x in rows]

    t["r_bps"] = 1e4 * t.r_distance / t.entry_price
    t["same_bar"] = (t.open_utc.dt.floor("5min") == t.close_utc.dt.floor("5min")).astype(int)
    # Мёртвый вход: позиция уже за стопом, едва открывшись.
    t["dead_on_arrival"] = (t.pos_at_open_r < -1).astype("Int64")
    # Недобор по выходу: сколько R осталось на столе после фиксации.
    t["left_on_table_r"] = t.mfe_after_exit_r - t.pnl_r
    return t


def per_experiment(t: pd.DataFrame) -> pd.DataFrame:
    g = t.groupby("experiment_id")
    m = pd.DataFrame({
        "trades": g.size(),
        "sum_r": g.pnl_r.sum(),
        "expectancy_r": g.pnl_r.mean(),
        "win_rate": g.is_winner.mean(),
        "avg_win_r": g.apply(lambda x: x.loc[x.is_winner == 1, "pnl_r"].mean(), include_groups=False),
        "avg_loss_r": g.apply(lambda x: x.loc[x.is_winner == 0, "pnl_r"].mean(), include_groups=False),
        "median_r_bps": g.r_bps.median(),
        "same_bar_share": g.same_bar.mean(),
        "dead_on_arrival_share": g.dead_on_arrival.mean(),
        "median_pos_at_open_r": g.pos_at_open_r.median(),
        "median_left_on_table_r": g.left_on_table_r.median(),
    })
    return m.reset_index()


# --- вывод ------------------------------------------------------------------

def git_rev() -> str:
    try:
        return subprocess.check_output(
            ["git", "rev-parse", "--short", "HEAD"], text=True, stderr=subprocess.DEVNULL
        ).strip()
    except Exception:
        return "unknown"


def report(t: pd.DataFrame, m: pd.DataFrame) -> None:
    n = len(t)
    print(f"Сделок: {n}   период: {t.trading_date.min()} → {t.trading_date.max()}")
    print(f"Итог: {t.pnl_r.sum():+.1f}R   expectancy: {t.pnl_r.mean():+.3f}R   "
          f"win rate: {t.is_winner.mean():.1%}")

    print("\n— Качество входа —")
    dead = t.dead_on_arrival.sum()
    print(f"  уже за стопом на открытии следующей свечи: {dead}/{t.pos_at_open_r.notna().sum()}"
          f" ({dead / max(t.pos_at_open_r.notna().sum(), 1):.0%})")
    print(f"  вход и выход в одной 5-мин свече:          {t.same_bar.sum()}/{n}"
          f" ({t.same_bar.mean():.0%})")
    print(f"  медиана позиции сразу после входа:         {t.pos_at_open_r.median():+.2f}R")
    if t.level_touched.notna().any():
        never = (t.level_touched == False).sum()  # noqa: E712
        print(f"  уровень пробоя не пройден в свече входа:   {never}/{t.level_touched.notna().sum()}")

    print("\n— Геометрия стопа —")
    print(f"  медиана R: {t.r_bps.median():.1f} б.п.   нижний квартиль: {t.r_bps.quantile(.25):.1f} б.п.")

    print("\n— Выход —")
    tp = t[t.close_reason == "TAKE_PROFIT"]
    if len(tp) and tp.left_on_table_r.notna().any():
        kept = tp.left_on_table_r.dropna()
        print(f"  выходов по тейку: {len(tp)}, взято в среднем {tp.pnl_r.mean():.2f}R")
        print(f"  осталось на столе до конца сессии: медиана {kept.median():+.2f}R, "
              f"дали ещё >1R: {(kept > 1).sum()}/{len(kept)}")
    else:
        print("  выходов по тейку нет")

    print("\n— По экспериментам —")
    cols = ["experiment_id", "trades", "sum_r", "expectancy_r", "win_rate",
            "median_r_bps", "same_bar_share", "dead_on_arrival_share"]
    print(m[cols].to_string(index=False, float_format=lambda v: f"{v:.3f}"))


def main() -> None:
    ap = argparse.ArgumentParser()
    ap.add_argument("--db", default="data/trades.db")
    ap.add_argument("--history", default="data/history")
    ap.add_argument("--out", default="data/analysis")
    ap.add_argument("--label", default="", help="пометка снимка в metrics.csv")
    ap.add_argument("--no-write", action="store_true", help="только отчёт, ничего не писать")
    args = ap.parse_args()

    t = load_trades(args.db)
    if t.empty:
        print("В БД нет закрытых сделок.")
        return
    t = enrich(t, load_history(args.history))
    m = per_experiment(t)
    report(t, m)

    if args.no_write:
        return

    os.makedirs(args.out, exist_ok=True)
    enriched_path = os.path.join(args.out, "trades_enriched.csv")
    t.to_csv(enriched_path, index=False)

    m.insert(0, "snapshot_at", datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"))
    m.insert(1, "git_rev", git_rev())
    m.insert(2, "label", args.label)
    m.insert(3, "date_from", t.trading_date.min())
    m.insert(4, "date_to", t.trading_date.max())

    metrics_path = os.path.join(args.out, "metrics.csv")
    header = not os.path.exists(metrics_path)
    m.to_csv(metrics_path, mode="a", header=header, index=False)
    print(f"\nЗаписано: {enriched_path}")
    print(f"Дописан снимок в: {metrics_path}")


if __name__ == "__main__":
    main()
