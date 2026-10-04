#!/usr/bin/env python3
"""Проверка or-fade на 25 тикерах с замороженными параметрами — docs/analysis/0012.

Каждый тикер — отдельный портфельный backtest (без конкуренции за счёт), 1 и 2 б.п. на ногу.

    python3 scripts/check-or-fade-universe.py --dry-run   # число сделок по группам
    python3 scripts/check-or-fade-universe.py             # отчёт и вердикт
"""
import argparse
import datetime as dt
import math
import os
import subprocess
import tempfile

import numpy as np
import pandas as pd
import yaml

CONFIG = "configs/legacy/runs/portfolio-paper-m5.yaml"
SLOT = "or-fade-conservative"
SLOT4 = ["LKOH", "CHMF", "MOEX", "AFKS"]
CORE6 = ["SBER", "GAZP", "NVTK", "ROSN", "MGNT", "TATN"]
REST15 = ["AFLT", "ALRS", "ENPG", "FEES", "FLOT", "IRAO", "MAGN", "MDMG", "MSNG", "MTSS",
          "NLMK", "PHOR", "PIKK", "POSI", "UPRO"]
PERIOD = {"core": ("2024-07-03", "2026-10-02"), "rest": ("2024-07-12", "2026-07-12")}
COMMON_END = dt.date(2026, 7, 12)
BLOCKS = [(dt.date(2024, 7, 1), dt.date(2024, 12, 31)), (dt.date(2025, 1, 1), dt.date(2025, 4, 30)),
          (dt.date(2025, 5, 1), dt.date(2025, 8, 31)), (dt.date(2025, 9, 1), dt.date(2025, 12, 31)),
          (dt.date(2026, 1, 1), dt.date(2026, 4, 30)), (dt.date(2026, 5, 1), dt.date(2026, 12, 31))]


def slot_config(ticker, workdir, slot_type):
    cfg = yaml.safe_load(open(CONFIG))
    exp = [e for e in cfg["experiments"] if e["id"] == SLOT][0]
    exp["tickers"] = [ticker]
    exp["strategy"]["type"] = slot_type
    cfg["experiments"] = [exp]
    path = os.path.join(workdir, f"{slot_type}-{ticker}.yaml")
    with open(path, "w") as f:
        yaml.safe_dump(cfg, f, allow_unicode=True, sort_keys=False)
    return path


def run(binary, config, slip, frm, to, out):
    subprocess.run([binary, "portfolio-backtest", "-config", config, "-date-from", frm, "-date-to", to,
                    "-slippage-bps", str(slip), "-trades-csv", out], check=True, capture_output=True, text=True)
    t = pd.read_csv(out)
    if t.empty:
        return t
    bar = pd.to_datetime(t["entry_bar_time"], utc=True).dt.tz_convert("Europe/Moscow")
    t["date"] = bar.dt.date
    t["weekend"] = bar.dt.weekday >= 5
    t["week"] = bar.dt.strftime("%G-%V")
    t["nth"] = t.groupby(["ticker", "date"]).cumcount() + 1
    t["block"] = t["date"].map(lambda d: next(i for i, (a, b) in enumerate(BLOCKS) if a <= d <= b))
    return t


def cluster_stats(t, col="net_r"):
    """Среднее, t и нижняя односторонняя 95%-граница с кластеризацией по неделе."""
    x = t[col].values
    n = len(x)
    if n < 2:
        return float("nan"), float("nan"), float("nan")
    mu = x.mean()
    g = pd.Series(x - mu).groupby(t["week"].values).sum()
    se = math.sqrt((g ** 2).sum()) / n
    return mu, mu / se, mu - 1.645 * se


def line(name, t):
    if t.empty:
        return f"  {name:28s} нет сделок"
    mu, tt, lb = cluster_stats(t)
    return f"  {name:28s} n={len(t):5d}  exp_R={mu:+.3f}  t={tt:+5.2f}  нижн.95%={lb:+.3f}  дат={t['date'].nunique()}"


def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--workdir", default=tempfile.mkdtemp(prefix="orfade-"))
    ap.add_argument("--dry-run", action="store_true")
    args = ap.parse_args()
    os.makedirs(args.workdir, exist_ok=True)
    binary = os.path.join(args.workdir, "optimizer")
    subprocess.run(["go", "build", "-o", binary, "./cmd/optimizer"], check=True)

    # Смена типа на session_or_fade (без фильтра тикеров) не должна менять сделки слота.
    for tk in SLOT4:
        frm, to = PERIOD["core"]
        a = run(binary, slot_config(tk, args.workdir, "opening_range_fade"), 1, frm, to, os.path.join(args.workdir, f"orig-{tk}.csv"))
        b = run(binary, slot_config(tk, args.workdir, "session_or_fade"), 1, frm, to, os.path.join(args.workdir, f"same-{tk}.csv"))
        cols = ["entry_bar_time", "direction", "entry_price", "exit_price", "net_r"]
        if len(a) != len(b) or not a[cols].reset_index(drop=True).equals(b[cols].reset_index(drop=True)):
            raise SystemExit(f"{tk}: session_or_fade даёт другие сделки, чем opening_range_fade — проверка невалидна")
    print("session_or_fade на тикерах слота совпадает с opening_range_fade поштучно.")

    trades = {1: [], 2: []}
    for tk in SLOT4 + CORE6 + REST15:
        frm, to = PERIOD["rest" if tk in REST15 else "core"]
        cfg = slot_config(tk, args.workdir, "session_or_fade")
        for slip in (1, 2):
            t = run(binary, cfg, slip, frm, to, os.path.join(args.workdir, f"{tk}-{slip}.csv"))
            if not t.empty:
                t["group"] = "слот-4" if tk in SLOT4 else ("ядро-6" if tk in CORE6 else "остальные-15")
                trades[slip].append(t)
    t1, t2 = pd.concat(trades[1], ignore_index=True), pd.concat(trades[2], ignore_index=True)

    if args.dry_run:
        print(t1.groupby("group").size().to_string())
        return

    common1, common2 = t1[t1.date <= COMMON_END], t2[t2.date <= COMMON_END]
    ctrl1 = common1[common1.group != "слот-4"]
    ctrl2 = common2[common2.group != "слот-4"]
    rest1 = common1[common1.group == "остальные-15"]

    print(f"\nОбщий период до {COMMON_END}, 1 б.п. на ногу:")
    for g in ["слот-4", "ядро-6", "остальные-15"]:
        print(line(g, common1[common1.group == g]))
    print(line("КОНТРОЛЬ (ядро-6 + остальные-15)", ctrl1))
    print(line("контроль при 2 б.п.", ctrl2))
    print(line("хвост ядра после 2026-07-12", t1[(t1.date > COMMON_END) & (t1.group != "остальные-15")]))

    print("\nСрезы контроля (1 б.п., описание):")
    for name, mask in [("BUY", ctrl1.direction == "BUY"), ("SELL", ctrl1.direction == "SELL"),
                       ("первая сделка дня", ctrl1.nth == 1), ("вторая сделка дня", ctrl1.nth >= 2),
                       ("будни", ~ctrl1.weekend), ("выходные", ctrl1.weekend)]:
        print(line(name, ctrl1[mask]))
    for i, (a, b) in enumerate(BLOCKS):
        print(line(f"блок {i + 1} {a:%Y-%m}…{min(b, COMMON_END):%Y-%m}", ctrl1[ctrl1.block == i]))
    print("\nПо тикерам контроля (1 б.п.):")
    print(ctrl1.groupby("ticker").net_r.agg(["size", "mean"]).round(3).sort_values("mean").to_string())

    loo_t = min(ctrl1[ctrl1.ticker != x].net_r.mean() for x in ctrl1.ticker.unique())
    loo_b = min(ctrl1[ctrl1.block != b].net_r.mean() for b in ctrl1.block.unique())
    mu_ctrl, _, lb_ctrl = cluster_stats(ctrl1)
    print(f"\nБез худшего тикера: {loo_t:+.3f}; без худшего блока: {loo_b:+.3f}")

    print("\nКритерии 0012:")
    insufficient = len(ctrl1) < 400 or len(rest1) < 200 or ctrl1.date.nunique() < 120
    if insufficient:
        print(f"  НЕДОСТАТОЧНО ДАННЫХ: контроль {len(ctrl1)}, остальные-15 {len(rest1)}, дат {ctrl1.date.nunique()}")
        return
    reasons = []
    if rest1.net_r.mean() <= 0:
        reasons.append(f"остальные-15 exp_R {rest1.net_r.mean():+.3f} ≤ 0")
    if ctrl2.net_r.mean() <= 0:
        reasons.append(f"контроль при 2 б.п. {ctrl2.net_r.mean():+.3f} ≤ 0")
    if loo_t <= 0 or loo_b <= 0:
        reasons.append(f"без одного тикера/блока {loo_t:+.3f}/{loo_b:+.3f}")
    if reasons:
        print("  НЕ ВЕРИТЬ: " + "; ".join(reasons))
    elif lb_ctrl > 0:
        print(f"  УСТОЙЧИВО НА ПОВТОРНО ИСПОЛЬЗОВАННОЙ ИСТОРИИ (нижняя граница {lb_ctrl:+.3f})")
    else:
        print(f"  НЕ ПОДТВЕРЖДЕНО (exp_R {mu_ctrl:+.3f}, нижняя граница {lb_ctrl:+.3f})")


if __name__ == "__main__":
    main()
