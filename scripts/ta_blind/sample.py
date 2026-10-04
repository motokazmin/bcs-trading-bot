"""Случайные точки решения. Запуск: python3 scripts/ta_blind/sample.py N SEED

Пишет points.json (тикер и время там есть — файл читает только render/settle, не человек).
"""
import json
import os
import random
import sys

import pandas as pd

from common import DECIDE_FROM, DECIDE_TO, TICKERS, WORK, load, points_path

FROM, TO = "2024-10-01", "2026-09-30"


def main():
    n, seed = int(sys.argv[1]), int(sys.argv[2])
    rng = random.Random(seed)
    os.makedirs(WORK, exist_ok=True)
    if os.path.exists(points_path()):
        sys.exit("points.json уже есть — точки генерируются один раз")
    cands = {}
    for t in TICKERS:
        df = load(t)
        end = df["end"]
        hhmm = end.dt.strftime("%H:%M")
        m = (end >= pd.Timestamp(FROM, tz=end.dt.tz)) & (end < pd.Timestamp(TO, tz=end.dt.tz)) \
            & (hhmm >= DECIDE_FROM) & (hhmm <= DECIDE_TO) & (end.dt.weekday < 5)
        cands[t] = list(end[m])
    pts = []
    for i in range(n):
        t = rng.choice(TICKERS)
        e = rng.choice(cands[t])
        pts.append({"id": f"p{i:03d}", "ticker": t, "t": e.isoformat()})
    with open(points_path(), "w") as f:
        json.dump(pts, f, indent=0)
    print(f"{n} точек → {points_path()}")


if __name__ == "__main__":
    main()
