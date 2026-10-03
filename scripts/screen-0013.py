#!/usr/bin/env python3
"""Скрининг трёх идей из литературы по замороженным правилам docs/analysis/0013-screen-preregistration-literature.md.

Данные, группы тикеров, издержки, блоки, t и Holm — общие с scripts/screen-hypotheses.py (0008).

    python3 scripts/screen-0013.py --dry-run     # только число сделок
    python3 scripts/screen-0013.py               # отчёт и вердикты
"""
import argparse
import importlib.util
import math
import os

import numpy as np
import pandas as pd

_spec = importlib.util.spec_from_file_location(
    "screen0008", os.path.join(os.path.dirname(os.path.abspath(__file__)), "screen-hypotheses.py"))
s8 = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(s8)

GROSS_MIN_BPS = 8.0
MIN_STOP_BPS = 20.0
EOD_BAR = 18 * 60 + 35
OR_BAR = 10 * 60
SIGNAL_LAST = 17 * 60 + 55


def prepare(d):
    """Индексы баров по будним датам, дневной ATR14 на вчера, RVOL бара 10:00."""
    daily = d.groupby("date").agg(H=("high", "max"), L=("low", "min"), C=("close", "last"))
    cp = daily["C"].shift()
    tr = np.maximum.reduce([daily["H"] - daily["L"], (daily["H"] - cp).abs(), (daily["L"] - cp).abs()])
    atr = pd.Series(tr, index=daily.index).rolling(14).mean().shift(1)
    atr_frac = (atr / daily["C"].shift()).to_dict()

    wd = d[d["weekday"] < 5]
    bars = {date: dict(zip(g["minute"].values, g.index.values)) for date, g in wd.groupby("date")}
    first = wd[wd["minute"] == OR_BAR].set_index("date")["volume"]
    rvol = (first / first.shift(1).rolling(14, min_periods=10).mean()).to_dict()
    return bars, atr_frac, rvol


def trade(d, e, x, dirn, stop_bps):
    """Вход по open бара e, стоп stop_bps, выход по close бара x."""
    o, h, l, c = d["open"].values, d["high"].values, d["low"].values, d["close"].values
    entry = o[e]
    sl = entry * (1 - dirn * stop_bps / 1e4)
    for j in range(e, x + 1):
        if j > e and (o[j] - sl) * dirn <= 0:
            return dirn * (o[j] / entry - 1) * 1e4
        adverse = l[j] if dirn > 0 else h[j]
        if (adverse - sl) * dirn <= 0:
            return dirn * (sl / entry - 1) * 1e4
    return dirn * (c[x] / entry - 1) * 1e4


def orb_trades(d, prep, start, end, rvol_min):
    bars_by_date, atr_frac, rvol = prep
    o, c = d["open"].values, d["close"].values
    hi_all, lo_all = d["high"].values, d["low"].values
    dates = d["date"].values
    rows = []
    for date, bars in bars_by_date.items():
        if not (start <= date <= end):
            continue
        i0 = bars.get(OR_BAR)
        if i0 is None or c[i0] == o[i0] or not np.isfinite(atr_frac.get(date, np.nan)):
            continue
        if rvol_min is not None and not (rvol.get(date, 0) >= rvol_min):
            continue
        dirn = 1 if c[i0] > o[i0] else -1
        level = hi_all[i0] if dirn > 0 else lo_all[i0]
        sig = None
        for m in sorted(k for k in bars if OR_BAR < k <= SIGNAL_LAST):
            i = bars[m]
            if (c[i] - level) * dirn > 0:
                sig = i
                break
        if sig is None:
            continue
        e = sig + 1
        if e >= len(d) or dates[e] != date:
            continue
        x = bars.get(EOD_BAR)
        if x is None:
            x = max(i for m, i in bars.items() if m < EOD_BAR)
        if x < e:
            continue
        stop = max(0.10 * atr_frac[date] * 1e4, MIN_STOP_BPS)
        rows.append({"ticker": d["ticker"].iat[0], "date": date, "gross": trade(d, e, x, dirn, stop)})
    return rows


def g3_trades(frames, core, basket, start, end, min_basket):
    """Моментум индекса: знак r1 по ядру → корзина basket с 18:10 до 18:35."""
    preps = {t: prepare(frames[t])[0] for t in set(core) | set(basket)}

    def prev_main_close(t, date):
        dates = sorted(k for k in preps[t] if k < date)
        if not dates:
            return None
        bars = preps[t][dates[-1]]
        mins = [m for m in bars if m <= EOD_BAR]
        return frames[t]["close"].values[bars[max(mins)]] if mins else None

    all_dates = sorted(set().union(*(preps[t].keys() for t in core)))
    rows = []
    for date in all_dates:
        if not (start <= date <= end):
            continue
        r = []
        for t in core:
            bars = preps[t].get(date, {})
            i = bars.get(10 * 60 + 25)
            pc = prev_main_close(t, date) if i is not None else None
            if i is not None and pc:
                r.append(frames[t]["close"].values[i] / pc - 1)
        if len(r) < 8 or np.mean(r) == 0:
            continue
        sign = 1 if np.mean(r) > 0 else -1
        rets = []
        for t in basket:
            bars = preps[t].get(date, {})
            a, b = bars.get(18 * 60 + 10), bars.get(EOD_BAR)
            if a is not None and b is not None:
                rets.append(frames[t]["close"].values[b] / frames[t]["open"].values[a] - 1)
        if len(rets) < min_basket:
            continue
        rows.append({"ticker": "корзина", "date": date, "gross": sign * np.mean(rets) * 1e4})
    return rows


def summarize(tr, start, end):
    c1, c2 = s8.cost_bps(s8.SLIPPAGE_BPS[0]), s8.cost_bps(s8.SLIPPAGE_BPS[1])
    net1 = tr["gross"] - c1
    blocks = s8.block_of(tr["date"], start, end)
    by_block = net1.groupby(blocks).mean().reindex(range(s8.N_BLOCKS))
    t = s8.cluster_t(net1.values, tr["date"].values)
    loo_t = (min(net1[tr["ticker"] != x].mean() for x in tr["ticker"].unique())
             if tr["ticker"].nunique() > 1 else float("nan"))
    loo_b = min(net1[blocks != b].mean() for b in range(s8.N_BLOCKS) if (blocks != b).any())
    return {"n": len(tr), "gross": tr["gross"].mean(), "net1": net1.mean(), "net2": (tr["gross"] - c2).mean(),
            "t": t, "p": s8.one_sided_p(t), "blocks_pos": int((by_block > 0).sum()),
            "loo_ticker": loo_t, "loo_block": loo_b,
            "block_n": np.bincount(blocks, minlength=s8.N_BLOCKS).tolist()}


def fmt(s):
    loo_t = "—" if math.isnan(s["loo_ticker"]) else f"{s['loo_ticker']:+.2f}"
    return (f"n={s['n']:5d}  gross={s['gross']:+6.2f}  net@3.6={s['net1']:+6.2f}  net@5.6={s['net2']:+6.2f}  "
            f"t={s['t']:+5.2f}  блоков+ {s['blocks_pos']}/6\n"
            f"           без худшего тикера {loo_t}, без худшего блока {s['loo_block']:+.2f}; по блокам {s['block_n']}")


def verdict(code, core, check, holm_ok, g1_net):
    if core["n"] < 200 or core["gross"] < GROSS_MIN_BPS or core["net2"] <= 0:
        return "БРОСИТЬ"
    loo_t_ok = math.isnan(core["loo_ticker"]) or core["loo_ticker"] > 0
    ok = (core["blocks_pos"] >= 4 and loo_t_ok and core["loo_block"] > 0 and holm_ok
          and check is not None and check["n"] >= 200 and check["net1"] > 0)
    if code == "G2":
        ok = ok and core["net1"] > g1_net
    return "КАНДИДАТ" if ok else "не опровергнута, не доказана"


def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--history-dir", default="data/history")
    ap.add_argument("--dry-run", action="store_true")
    args = ap.parse_args()
    frames = s8.load(args.history_dir)
    for t, d in frames.items():
        d["ticker"] = t
    preps = {t: prepare(d) for t, d in frames.items()}

    results = {}
    for code, rvol_min in (("G1", None), ("G2", 2.0)):
        results[code] = {}
        for g, tickers in s8.GROUPS.items():
            start, end = s8.PERIODS[g]
            rows = [r for t in tickers for r in orb_trades(frames[t], preps[t], start, end, rvol_min)]
            results[code][g] = (pd.DataFrame(rows), start, end)
    results["G3"] = {}
    for g, tickers in s8.GROUPS.items():
        start, end = s8.PERIODS[g]
        rows = g3_trades(frames, s8.CORE, tickers, start, end, 8 if g == "ядро-10" else 10)
        results["G3"][g] = (pd.DataFrame(rows), start, end)

    names = {"G1": "пробой первого 5-минутного бара до EOD", "G2": "то же, RVOL ≥ 2 («бумаги в игре»)",
             "G3": "моментум индекса: 10:30 → 18:10–18:40"}
    if args.dry_run:
        for code, pg in results.items():
            print(f"{code} {names[code]}: " + ", ".join(f"{g}: {len(tr)}" for g, (tr, _, _) in pg.items()))
        return

    stats = {code: {g: summarize(tr, st, en) if len(tr) else None for g, (tr, st, en) in pg.items()}
             for code, pg in results.items()}
    order = ["G1", "G2", "G3"]
    holm_ok = dict(zip(order, s8.holm([stats[c]["ядро-10"]["p"] for c in order])))

    print("Скрининг по 0013. Вход по open, издержки 3.6 / 5.6 б.п. на круг.\n")
    for code in order:
        print(f"{code} — {names[code]}")
        for g in s8.GROUPS:
            print(f"  {g:12s}" + (fmt(stats[code][g]) if stats[code][g] else "нет сделок"))
        core, check = stats[code]["ядро-10"], stats[code]["проверка-15"]
        print(f"  Holm: {'проходит' if holm_ok[code] else 'не проходит'} (p={core['p']:.4f})")
        print(f"  ВЕРДИКТ: {verdict(code, core, check, holm_ok[code], stats['G1']['ядро-10']['net1'])}\n")


if __name__ == "__main__":
    main()
