"""Исход решений из журнала. Запуск только после того, как решения записаны.

python3 scripts/ta_blind/settle.py [--report]

Модель исполнения (консервативная, как у бота):
- вход — open первого M5-бара после момента решения, проскальзывание против позиции;
- стоп и тейк фиксированы, проверяются по high/low M5; гэп через уровень — по open;
- стоп и тейк задеты в одном баре — считается стоп;
- выход по времени — close бара, кончающегося в 18:40;
- комиссия 0.008% и проскальзывание 1 б.п. на каждой ноге.
R = чистый результат / риск до стопа (от фактической цены входа).
Зеркало — та же геометрия (расстояния до стопа и тейка) в обратную сторону от того же входа.
"""
import json
import math
import os
import sys

import pandas as pd

from common import COMMISSION, EOD, SLIPPAGE, WORK, load, load_journal, load_points


def simulate(bars, side, entry_raw, stop_d, tgt_d):
    """side +1/−1; stop_d, tgt_d — расстояния в долях от цены входа. Возвращает (R, причина)."""
    entry = entry_raw * (1 + side * SLIPPAGE)
    stop = entry * (1 - side * stop_d)
    tgt = entry * (1 + side * tgt_d)
    exit_px, why = None, "eod"
    for i, b in enumerate(bars.itertuples()):
        o, h, l = b.open, b.high, b.low
        if i > 0:  # гэп на открытии бара
            if (side > 0 and o <= stop) or (side < 0 and o >= stop):
                exit_px, why = o, "stop"
                break
            if (side > 0 and o >= tgt) or (side < 0 and o <= tgt):
                exit_px, why = o, "target"
                break
        hit_stop = (l <= stop) if side > 0 else (h >= stop)
        hit_tgt = (h >= tgt) if side > 0 else (l <= tgt)
        if hit_stop:
            exit_px, why = stop, "stop"
            break
        if hit_tgt:
            exit_px, why = tgt, "target"
            break
    if exit_px is None:
        exit_px = bars["close"].iloc[-1]
    exit_eff = exit_px * (1 - side * SLIPPAGE)
    gross = side * (exit_eff - entry) / entry
    net = gross - 2 * COMMISSION
    return net / stop_d, why


def settle_one(p, d):
    t = pd.Timestamp(p["t"])
    df = load(p["ticker"])
    eod = pd.Timestamp(f"{t.date()} {EOD}", tz=t.tz)
    fut = df[(df["ts"] >= t) & (df["end"] <= eod)]
    if fut.empty:
        return None
    # Уровни в журнале — на нормированной шкале (close момента решения = 100).
    last_close = df[df["end"] <= t]["close"].iloc[-1]
    side = 1 if d["action"] == "long" else -1
    ref = 100.0
    stop_d = abs(ref - d["stop"]) / ref
    tgt_d = abs(d["target"] - ref) / ref
    if (side > 0 and not (d["stop"] < ref < d["target"])) or (side < 0 and not (d["target"] < ref < d["stop"])):
        raise SystemExit(f"{p['id']}: стоп/тейк не по разные стороны от 100 для {d['action']}")
    entry_raw = fut["open"].iloc[0]
    r, why = simulate(fut, side, entry_raw, stop_d, tgt_d)
    rm, whym = simulate(fut, -side, entry_raw, stop_d, tgt_d)
    return {"id": p["id"], "action": d["action"], "setup": d.get("setup", ""), "r": r, "why": why,
            "mirror_r": rm, "stop_bps": stop_d * 1e4, "rr": tgt_d / stop_d,
            "gap_entry_bps": (entry_raw / last_close - 1) * 1e4}


def main():
    pts, jr = load_points(), load_journal()
    rows = []
    for pid, d in sorted(jr.items()):
        if d["action"] not in ("long", "short"):
            continue
        res = settle_one(pts[pid], d)
        if res:
            rows.append(res)
    out = pd.DataFrame(rows)
    out.to_csv(os.path.join(WORK, "settled.csv"), index=False)
    n_dec = len(jr)
    n = len(out)
    print(f"решений {n_dec}, сделок {n}, пропусков {n_dec - n}")
    if n == 0:
        return
    se = out["r"].std(ddof=1) / math.sqrt(n) if n > 1 else float("nan")
    diff = out["r"] - out["mirror_r"]
    se_d = diff.std(ddof=1) / math.sqrt(n) if n > 1 else float("nan")
    print(f"exp_R {out['r'].mean():+.3f} ± {se:.3f} (SE)   win {100*(out['r']>0).mean():.1f}%   sum {out['r'].sum():+.1f}R")
    print(f"зеркало exp_R {out['mirror_r'].mean():+.3f}   разница {diff.mean():+.3f} ± {se_d:.3f} (t = {diff.mean()/se_d:+.2f})")
    print(f"стоп медиана {out['stop_bps'].median():.0f} б.п., RR медиана {out['rr'].median():.2f}")
    print("выходы:", out["why"].value_counts().to_dict())
    if "--report" in sys.argv:
        print("\nпо сетапам:")
        print(out.groupby("setup")["r"].agg(["count", "mean"]).sort_values("count", ascending=False).to_string())
        print("\nпо направлению:")
        print(out.groupby("action")["r"].agg(["count", "mean"]).to_string())


if __name__ == "__main__":
    main()
