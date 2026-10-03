#!/usr/bin/env python3
"""Скрининг трёх гипотез по замороженным правилам docs/analysis/0011-screen-preregistration-fable.md.

Данные, группы тикеров, издержки, блоки, t и Holm — общие с scripts/screen-hypotheses.py (0008).

    python3 scripts/screen-0011.py --dry-run     # только число сделок
    python3 scripts/screen-0011.py               # отчёт и вердикты
"""
import argparse
import importlib.util
import os

import numpy as np
import pandas as pd

_spec = importlib.util.spec_from_file_location(
    "screen0008", os.path.join(os.path.dirname(os.path.abspath(__file__)), "screen-hypotheses.py"))
s8 = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(s8)

STOP_BPS = 40.0
GROSS_MIN_BPS = 8.0


def hm(h, m):
    return h * 60 + m


def day_index(d):
    """{дата: {минута начала бара: индекс строки}} — только будни."""
    out = {}
    for date, g in d[d["weekday"] < 5].groupby("date"):
        out[date] = dict(zip(g["minute"].values, g.index.values))
    return out


def trade(d, bars, entry_min, exit_min, dirn):
    """Вход по open бара entry_min, стоп STOP_BPS, выход по open бара exit_min. None — нет бара входа."""
    e = bars.get(entry_min)
    if e is None:
        return None
    o, h, l, c = d["open"].values, d["high"].values, d["low"].values, d["close"].values
    x = bars.get(exit_min)
    if x is not None:
        last, exit_px = x - 1, o[x]
    else:
        before = [i for m, i in bars.items() if entry_min <= m < exit_min]
        last = max(before)
        exit_px = c[last]
    entry = o[e]
    sl = entry * (1 - dirn * STOP_BPS / 1e4)
    for j in range(e, last + 1):
        if j > e and (o[j] - sl) * dirn <= 0:
            return dirn * (o[j] / entry - 1) * 1e4
        adverse = l[j] if dirn > 0 else h[j]
        if (adverse - sl) * dirn <= 0:
            return dirn * (sl / entry - 1) * 1e4
    return dirn * (exit_px / entry - 1) * 1e4


# ---------------------------------------------------------------- гипотезы
# Каждая: (d, bars одной даты) → список (dirn, entry_min, exit_min, extra) или [].

def f1_morning(d, bars, date):
    if date < s8.MORNING_FROM:
        return []
    i0, i9, i10 = bars.get(hm(7, 0)), bars.get(hm(9, 45)), bars.get(hm(10, 0))
    if i0 is None or i9 is None or i10 is None:
        return []
    m = d["close"].values[i9] / d["open"].values[i0] - 1
    first = np.sign(d["close"].values[i10] - d["open"].values[i10])
    if abs(m) < 0.006 or first == 0 or first != -np.sign(m):
        return []
    dirn = int(-np.sign(m))
    pre = None
    e = bars.get(hm(10, 5))
    if e is not None:
        pre = dirn * (d["open"].values[e] / d["close"].values[i9] - 1) * 1e4
    return [(dirn, hm(10, 5), hm(11, 5), pre)]


def unwind_at(entry_h):
    """F2 с точкой входа entry_h:30 (17 — основная, 13 и 15 — диагностика)."""
    def f(d, bars, date):
        i_open = bars.get(hm(10, 0))
        i_sig = bars.get(hm(entry_h, 25))
        i_30 = bars.get(hm(entry_h - 1, 55))
        if i_open is None or i_sig is None or i_30 is None:
            return []
        c = d["close"].values
        day = c[i_sig] / d["open"].values[i_open] - 1
        r30 = c[i_sig] / c[i_30] - 1
        if abs(day) < 0.01 or day * r30 >= 0:
            return []
        return [(int(-np.sign(day)), hm(entry_h, 30), hm(entry_h + 1, 30), None)]
    return f


def f3_fade(d, bars, date):
    orb = [bars.get(hm(10, m)) for m in range(0, 30, 5)]
    if any(i is None for i in orb):
        return []
    hi = d["high"].values[orb].max()
    lo = d["low"].values[orb].min()
    c = d["close"].values
    for minute in range(hm(10, 30), hm(11, 55) + 1, 5):
        i = bars.get(minute)
        if i is None:
            continue
        if c[i] > hi or c[i] < lo:
            dirn = -1 if c[i] > hi else 1
            return [(dirn, minute + 5, minute + 5 + 30, None)]
    return []


HYPOTHESES = [
    ("F1", "разворот утра после 10:00", f1_morning),
    ("F2", "сворачивание дня 17:30–18:30", unwind_at(17)),
    ("F3", "немедленный фейд пробоя ORB", f3_fade),
]
DIAGNOSTICS = [("F2@13:30", unwind_at(13)), ("F2@15:30", unwind_at(15))]


def run(frames, tickers, fn, start, end):
    rows = []
    for t in tickers:
        d = frames[t]
        for date, bars in day_index(d).items():
            if not (start <= date <= end):
                continue
            for dirn, entry_min, exit_min, extra in fn(d, bars, date):
                g = trade(d, bars, entry_min, exit_min, dirn)
                if g is not None:
                    rows.append({"ticker": t, "date": date, "dir": dirn, "gross": g, "pre": extra})
    return pd.DataFrame(rows)


def summarize(tr, start, end):
    c1, c2 = s8.cost_bps(s8.SLIPPAGE_BPS[0]), s8.cost_bps(s8.SLIPPAGE_BPS[1])
    net1 = tr["gross"] - c1
    blocks = s8.block_of(tr["date"], start, end)
    by_block = net1.groupby(blocks).mean().reindex(range(s8.N_BLOCKS))
    t = s8.cluster_t(net1.values, tr["date"].values)
    loo_ticker = min(net1[tr["ticker"] != x].mean() for x in tr["ticker"].unique()) if tr["ticker"].nunique() > 1 else float("nan")
    loo_block = min(net1[blocks != b].mean() for b in range(s8.N_BLOCKS))
    return {
        "n": len(tr), "gross": tr["gross"].mean(), "net1": net1.mean(), "net2": (tr["gross"] - c2).mean(),
        "t": t, "p": s8.one_sided_p(t), "blocks_pos": int((by_block > 0).sum()),
        "loo_ticker": loo_ticker, "loo_block": loo_block,
        "block_n": np.bincount(blocks, minlength=s8.N_BLOCKS).tolist(),
        "pre": tr["pre"].dropna().mean() if tr["pre"].notna().any() else None,
    }


def fmt(s):
    line = (f"n={s['n']:5d}  gross={s['gross']:+6.2f}  net@3.6={s['net1']:+6.2f} (≈{s['net1'] / STOP_BPS:+.3f}R)  "
            f"net@5.6={s['net2']:+6.2f}  t={s['t']:+5.2f}  блоков+ {s['blocks_pos']}/6\n"
            f"           без худшего тикера {s['loo_ticker']:+.2f}, без худшего блока {s['loo_block']:+.2f}; "
            f"по блокам {s['block_n']}")
    if s["pre"] is not None:
        line += f"\n           ход до входа (close 09:45 → open 10:05, в сторону сделки): {s['pre']:+.2f} б.п."
    return line


def verdict(code, core, check, holm_ok, diag_net):
    if core["n"] < 200 or core["gross"] < GROSS_MIN_BPS or core["net2"] <= 0:
        return "БРОСИТЬ"
    ok = (core["blocks_pos"] >= 4 and core["loo_ticker"] > 0 and core["loo_block"] > 0 and holm_ok
          and check is not None and check["n"] >= 200 and check["net1"] > 0)
    if code == "F2":
        ok = ok and all(core["net1"] > v for v in diag_net)
    return "КАНДИДАТ" if ok else "не опровергнута, не доказана"


def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--history-dir", default="data/history")
    ap.add_argument("--dry-run", action="store_true")
    args = ap.parse_args()
    frames = s8.load(args.history_dir)

    def per_group(fn, code):
        out = {}
        for g, tickers in s8.GROUPS.items():
            start, end = s8.PERIODS[g]
            if code == "F1":
                start = max(start, s8.MORNING_FROM)
            out[g] = (run(frames, tickers, fn, start, end), start, end)
        return out

    results = [(code, name, per_group(fn, code)) for code, name, fn in HYPOTHESES]
    diags = [(code, per_group(fn, code)) for code, fn in DIAGNOSTICS]

    if args.dry_run:
        for code, name, pg in results + [(c, "диагностика", pg) for c, pg in diags]:
            print(f"{code} {name}: " + ", ".join(f"{g}: {len(tr)}" for g, (tr, _, _) in pg.items()))
        return

    stats = [(code, name, {g: summarize(tr, st, en) if len(tr) else None for g, (tr, st, en) in pg.items()})
             for code, name, pg in results]
    dstats = {code: {g: summarize(tr, st, en) if len(tr) else None for g, (tr, st, en) in pg.items()}
              for code, pg in diags}
    holm_ok = s8.holm([s["ядро-10"]["p"] for _, _, s in stats])

    print(f"Скрининг по 0011. Стоп {STOP_BPS:.0f} б.п., вход и выход по open, издержки 3.6 / 5.6 б.п. на круг.\n")
    for (code, name, s), hk in zip(stats, holm_ok):
        print(f"{code} — {name}")
        for g in s8.GROUPS:
            print(f"  {g:12s}" + (fmt(s[g]) if s[g] else "нет сделок"))
        diag_net = []
        if code == "F2":
            for dc, ds in dstats.items():
                for g in s8.GROUPS:
                    if ds[g]:
                        print(f"  {dc} {g:12s} n={ds[g]['n']:5d}  gross={ds[g]['gross']:+6.2f}  net@3.6={ds[g]['net1']:+6.2f}")
                diag_net.append(ds["ядро-10"]["net1"])
        print(f"  Holm: {'проходит' if hk else 'не проходит'} (p={s['ядро-10']['p']:.4f})")
        print(f"  ВЕРДИКТ: {verdict(code, s['ядро-10'], s['проверка-15'], hk, diag_net)}\n")


if __name__ == "__main__":
    main()
