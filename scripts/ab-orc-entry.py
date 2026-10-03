#!/usr/bin/env python3
"""A/B: вход ORC по close бара пробоя против ретест-лимита — docs/analysis/0009.

Плечо A — configs/runs/portfolio-paper.yaml как есть; плечо B — тот же файл с
`entry_at_close: true` у ORC-слотов. Оба плеча — портфельный backtest при 1 и 2 б.п.

    python3 scripts/ab-orc-entry.py [--workdir DIR]
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

CONFIG = "configs/runs/portfolio-paper.yaml"
ORC_TYPES = {"opening_range_continuation", "session_orc"}
DATE_FROM, DATE_TO = "2024-07-03", "2026-10-02"
START, END = dt.date(2024, 7, 3), dt.date(2026, 10, 2)
N_BLOCKS = 6


def make_arm_b(path):
    cfg = yaml.safe_load(open(CONFIG))
    slots = []
    for exp in cfg["experiments"]:
        if exp["strategy"]["type"] in ORC_TYPES:
            exp["strategy"]["entry_at_close"] = True
            slots.append(exp["id"])
    with open(path, "w") as f:
        yaml.safe_dump(cfg, f, allow_unicode=True, sort_keys=False)
    return slots


def run(binary, config, slip, out_csv):
    cmd = [binary, "portfolio-backtest", "-config", config, "-date-from", DATE_FROM, "-date-to", DATE_TO,
           "-slippage-bps", str(slip), "-trades-csv", out_csv]
    res = subprocess.run(cmd, capture_output=True, text=True, check=True)
    head = [l for l in res.stdout.splitlines() if l.startswith("trades=")][0]
    return head, load_trades(out_csv)


def load_trades(path):
    t = pd.read_csv(path)
    bar = pd.to_datetime(t["entry_bar_time"], utc=True)
    t["bar"] = bar
    t["date"] = bar.dt.tz_convert("Europe/Moscow").dt.date
    span = (END - START).days + 1
    t["block"] = np.clip(t["date"].map(lambda d: (d - START).days) * N_BLOCKS // span, 0, N_BLOCKS - 1)
    return t


def stats(t):
    if t.empty:
        return {"n": 0, "exp_r": float("nan"), "pf": float("nan")}
    loss = -t.loc[t.net_pnl < 0, "net_pnl"].sum()
    return {"n": len(t), "exp_r": t.net_r.mean(),
            "pf": t.loc[t.net_pnl > 0, "net_pnl"].sum() / loss if loss > 0 else float("nan")}


def blocks(t):
    return t.groupby("block").net_r.mean().reindex(range(N_BLOCKS))


def cluster_diff_t(y, d, groups):
    """t коэффициента при d в y = a + b·d с кластеризацией по дню."""
    X = np.column_stack([np.ones(len(y)), d.astype(float)])
    xtx_inv = np.linalg.inv(X.T @ X)
    beta = xtx_inv @ X.T @ y
    u = y - X @ beta
    meat = np.zeros((2, 2))
    for g in np.unique(groups):
        m = groups == g
        s = X[m].T @ u[m]
        meat += np.outer(s, s)
    v = xtx_inv @ meat @ xtx_inv
    return beta[1], beta[1] / math.sqrt(v[1, 1])


def tag_retest(t, history_dir):
    """Был ли ретест уровня после бара пробоя до конца часа (время жизни лимита в A)."""
    cache, out = {}, []
    for r in t.itertuples():
        if r.ticker not in cache:
            h = pd.read_csv(os.path.join(history_dir, f"{r.ticker}.csv"))
            h["ts"] = pd.to_datetime(h["timestamp"], utc=True)
            cache[r.ticker] = h.set_index("ts").sort_index()
        h = cache[r.ticker]
        end = r.bar.floor("h") + pd.Timedelta(hours=1)
        w = h[(h.index > r.bar) & (h.index < end)]
        if r.direction == "BUY":
            out.append(bool((w["low"] <= r.breakout_upper).any()))
        else:
            out.append(bool((w["high"] >= r.breakout_lower).any()))
    return np.array(out)


def fmt(s):
    return f"n={s['n']:4d} exp_R={s['exp_r']:+.3f} PF={s['pf']:.2f}"


def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--workdir", default=tempfile.mkdtemp(prefix="ab-orc-"))
    ap.add_argument("--history-dir", default="data/history")
    args = ap.parse_args()
    os.makedirs(args.workdir, exist_ok=True)

    binary = os.path.join(args.workdir, "optimizer")
    subprocess.run(["go", "build", "-o", binary, "./cmd/optimizer"], check=True)
    cfg_b = os.path.join(args.workdir, "arm-b.yaml")
    orc = make_arm_b(cfg_b)

    arms = {}
    for arm, cfg in (("A", CONFIG), ("B", cfg_b)):
        for slip in (1, 2):
            head, t = run(binary, cfg, slip, os.path.join(args.workdir, f"{arm}-{slip}.csv"))
            arms[(arm, slip)] = t
            print(f"[{arm} @{slip} б.п.] {head}")
    print(f"\nORC-слоты: {', '.join(orc)}. Сделки: {args.workdir}\n")

    for slip in (1, 2):
        print(f"— проскальзывание {slip} б.п. на ногу")
        for slot in orc + ["ORC вместе"]:
            row = []
            for arm in ("A", "B"):
                t = arms[(arm, slip)]
                t = t[t.experiment_id.isin(orc)] if slot == "ORC вместе" else t[t.experiment_id == slot]
                row.append(fmt(stats(t)))
            print(f"  {slot:22s} A: {row[0]}   B: {row[1]}")
        others = [e for e in arms[("A", slip)].experiment_id.unique() if e not in orc]
        for slot in sorted(others):
            a = stats(arms[("A", slip)][lambda x: x.experiment_id == slot])
            b = stats(arms[("B", slip)][lambda x: x.experiment_id == slot])
            print(f"  {slot:22s} A: {fmt(a)}   B: {fmt(b)}   (не ORC, сдвиг через общий счёт)")
        print()

    a1 = arms[("A", 1)][lambda x: x.experiment_id.isin(orc)]
    b1 = arms[("B", 1)][lambda x: x.experiment_id.isin(orc)].copy()
    ba, bb = blocks(a1), blocks(b1)
    print("Блоки, ORC вместе, 1 б.п.:")
    print("  A     " + " ".join(f"{v:+.3f}" for v in ba))
    print("  B     " + " ".join(f"{v:+.3f}" for v in bb))
    print("  B − A " + " ".join(f"{v:+.3f}" for v in bb - ba))
    print(f"  блоков B > A: {int((bb > ba).sum())}/6, блоков B > 0: {int((bb > 0).sum())}/6\n")

    b1["retest"] = tag_retest(b1, args.history_dir)
    yes, no = b1[b1.retest], b1[~b1.retest]
    diff, t = cluster_diff_t(b1.net_r.values, b1.retest.values,
                             np.array([str(d) for d in b1.date]))
    print("Разложение отбора, плечо B, 1 б.п.:")
    print(f"  ретест был:     {fmt(stats(yes))}  (доля {len(yes) / len(b1):.0%})")
    print(f"  ретеста не было: {fmt(stats(no))}")
    print(f"  разность (был − не было): {diff:+.3f}R, t={t:+.2f} (кластеры по дню)")
    print(f"  A (лимит) против B «ретест был»: {stats(a1)['exp_r']:+.3f} против {stats(yes)['exp_r']:+.3f}")

    sa, sb, sb2 = stats(a1)["exp_r"], stats(b1)["exp_r"], stats(arms[("B", 2)][lambda x: x.experiment_id.isin(orc)])["exp_r"]
    c1 = sb > sa and (bb > ba).sum() >= 4
    c2 = c1 and sb >= 0.08 and sb2 >= 0 and (bb > 0).sum() >= 4
    c3 = diff < 0 and t <= -2
    print("\nКритерии 0009:")
    print(f"  1. ретест-лимит уступает close-входу: {'да' if c1 else 'нет'}")
    print(f"  2. close-вход — кандидат на переподбор: {'да' if c2 else 'нет'}")
    print(f"  3. отрицательный отбор подтверждён: {'да' if c3 else 'нет'}")
    if not c1 and not c3:
        print("  4. гипотеза ревью не подтверждена")


if __name__ == "__main__":
    main()
