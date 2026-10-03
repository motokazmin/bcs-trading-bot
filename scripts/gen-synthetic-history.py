#!/usr/bin/env python3
"""Синтетическая M5-история — мартингал для нулевой модели (docs/analysis/0006, 0007).

Цена — случайное блуждание без дрейфа и автокорреляции, собранное из STEPS шагов
на бар в OHLC. На таком рынке матожидание ЛЮБОЙ стратегии до издержек ровно 0:
плюс выдаёт заглядывание вперёд в симуляторе или стратегии, а не преимущество.

Бары 07:00–23:50 МСК (04:00Z–20:45Z), будни, случайный объём (для фильтров по
объёму). Шагов на бар — тысячи: при 300 перескок через уровень даёт ~0.025R на
пересечение и маскирует перекосы такого же размера.

    python3 scripts/gen-synthetic-history.py /tmp/synth --days 700 --seed 11
    go run ./cmd/optimizer portfolio-backtest -history-dir /tmp/synth -slippage-bps 0

exp_R в выводе portfolio-backtest — после комиссии, поэтому ожидание на синтетике
не 0, а минус комиссия в R (~−0.06 при стопе ~25 б.п.). Чистый ноль — по
Trades[].PnLR (в backtest он до комиссии), см. TestLiveMatchesBacktestOnHistory
как пример обвязки.
"""
import argparse
import datetime as dt

import numpy as np

TICKERS = ["MGNT", "ROSN", "TATN", "LKOH", "CHMF", "MOEX", "AFKS", "NVTK", "GAZP", "SBER"]

ap = argparse.ArgumentParser()
ap.add_argument("out")
ap.add_argument("--days", type=int, default=700)
ap.add_argument("--seed", type=int, default=11)
ap.add_argument("--steps", type=int, default=3000, help="шагов на бар")
ap.add_argument("--bar-sigma", type=float, default=0.0018, help="σ лог-доходности бара (~18 б.п.: стопы проходят min_stop_bps 20)")
args = ap.parse_args()

rng = np.random.default_rng(args.seed)
days, d = [], dt.date(2022, 1, 3)
while len(days) < args.days:
    if d.weekday() < 5:
        days.append(d)
    d += dt.timedelta(days=1)

NB = 202  # 04:00Z … 20:45Z
step_sigma = args.bar_sigma / np.sqrt(args.steps)
for t in TICKERS:
    rows = ["timestamp,open,high,low,close,volume"]
    logp = np.log(100.0)
    for day in days:
        path = logp + np.cumsum(rng.normal(0, step_sigma, size=NB * args.steps)).reshape(NB, args.steps)
        prev = np.concatenate([[logp], path[:-1, -1]])
        o, c = np.exp(prev), np.exp(path[:, -1])
        h = np.maximum(np.exp(path.max(1)), np.maximum(o, c))
        lo = np.minimum(np.exp(path.min(1)), np.minimum(o, c))
        v = rng.lognormal(13, 0.8, NB).astype(np.int64)
        logp = path[-1, -1]
        base = dt.datetime(day.year, day.month, day.day, 4, 0)
        for i in range(NB):
            ts = (base + dt.timedelta(minutes=5 * i)).strftime("%Y-%m-%dT%H:%M:%SZ")
            rows.append(f"{ts},{o[i]:.6f},{h[i]:.6f},{lo[i]:.6f},{c[i]:.6f},{v[i]}")
    with open(f"{args.out}/{t}.csv", "w") as f:
        f.write("\n".join(rows) + "\n")
