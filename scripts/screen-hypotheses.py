#!/usr/bin/env python3
"""Скрининг пяти гипотез по замороженным правилам docs/analysis/0008-screen-preregistration.md.

Правила, пороги и критерии — там; здесь только их исполнение. Менять пороги в этом файле
после первого прогона нельзя: новая версия гипотезы — новый файл регистрации.

    python3 scripts/screen-hypotheses.py --dry-run     # только число сигналов, без доходности
    python3 scripts/screen-hypotheses.py               # полный отчёт и вердикты
    python3 scripts/screen-hypotheses.py --trades data/analysis/screen-0008-trades.csv
"""
import argparse
import datetime as dt
import math
import os

import numpy as np
import pandas as pd

CORE = ["SBER", "GAZP", "LKOH", "ROSN", "NVTK", "TATN", "MGNT", "CHMF", "MOEX", "AFKS"]
CHECK = ["AFLT", "ALRS", "ENPG", "FEES", "FLOT", "IRAO", "MAGN", "MDMG", "MSNG", "MTSS",
         "NLMK", "PHOR", "PIKK", "POSI", "UPRO"]
GROUPS = {"ядро-10": CORE, "проверка-15": CHECK}
PERIODS = {
    "ядро-10": (dt.date(2024, 7, 3), dt.date(2026, 10, 2)),
    "проверка-15": (dt.date(2024, 7, 12), dt.date(2026, 7, 12)),
}
MORNING_FROM = dt.date(2025, 1, 27)  # утренняя сессия в данных — с этой даты

COMMISSION_BPS = 0.8  # на ногу
SLIPPAGE_BPS = (1.0, 2.0)  # на ногу: основной и стресс
MIN_STOP_BPS = 20.0
N_BLOCKS = 6

MAIN_LAST = 18 * 60 + 35  # последний бар основной сессии начинается в 18:35
MORNING_LAST = 9 * 60 + 45
MAIN_SIGNAL = (10 * 60 + 15, 18 * 60 + 15)  # H2–H4: начало сигнального бара


def cost_bps(slip):
    return 2 * (COMMISSION_BPS + slip)


# ---------------------------------------------------------------- данные

def load(history_dir):
    frames = {}
    for t in CORE + CHECK:
        d = pd.read_csv(os.path.join(history_dir, f"{t}.csv"))
        ts = pd.to_datetime(d["timestamp"], utc=True).dt.tz_convert("Europe/Moscow")
        d["ts"] = ts
        d["date"] = ts.dt.date
        d["minute"] = ts.dt.hour * 60 + ts.dt.minute
        d["weekday"] = ts.dt.weekday
        d = d.sort_values("ts").reset_index(drop=True)
        pc = d["close"].shift()
        tr = np.maximum.reduce([d["high"] - d["low"], (d["high"] - pc).abs(), (d["low"] - pc).abs()])
        d["atr"] = pd.Series(tr).rolling(14).mean()  # 14 закрытых баров, включая текущий
        d["atr_bps"] = d["atr"] / d["close"] * 1e4
        frames[t] = d
    return frames


def lagged_close(d, minutes):
    """close ровно `minutes` минут назад (NaN, если такого бара нет)."""
    s = pd.Series(d["close"].values, index=d["ts"])
    return s.reindex(d["ts"] - pd.Timedelta(minutes=minutes)).values


def core_market(frames, col):
    """Медиана `col` по ядру-10 на каждой метке; NaN, если тикеров меньше 8."""
    wide = pd.DataFrame({t: pd.Series(frames[t][col].values, index=frames[t]["ts"]) for t in CORE})
    med = wide.median(axis=1)
    med[wide.notna().sum(axis=1) < 8] = np.nan
    return wide, med


# ---------------------------------------------------------------- сигналы
# Каждая функция возвращает DataFrame: idx, dir, stop_bps (стоп правила), rr, target (минута выхода).

def in_main_window(d):
    return (d["weekday"] < 5) & d["minute"].between(*MAIN_SIGNAL)


def h1_gap(d, ctx):
    last = d.groupby("date")["close"].last()
    prev_close = last.shift()  # close последнего бара предыдущей даты с барами
    m = d[(d["minute"] == 7 * 60) & (d["weekday"] < 5) & (d["date"] >= MORNING_FROM)].copy()
    m["prev"] = m["date"].map(prev_close)
    gap = m["open"] / m["prev"] - 1
    move = m["close"] - m["prev"]
    ok = (gap.abs() >= 0.004) & (np.sign(move) == np.sign(gap)) & (move.abs() >= 0.5 * gap.abs() * m["prev"])
    s = m[ok]
    return pd.DataFrame({"idx": s.index, "dir": np.sign(gap[ok]).astype(int).values,
                         "stop_bps": (0.5 * gap[ok].abs() * 1e4).values, "rr": 1.5,
                         "target": 7 * 60 + 5 * 6, "cap": MORNING_LAST})


def h2_volume(d, ctx):
    wd = d[d["weekday"] < 5]
    vmed = wd.groupby("minute")["volume"].transform(
        lambda s: s.shift(1).rolling(20, min_periods=10).median())
    vmed = vmed.reindex(d.index)
    ret = d["close"] / d["open"] - 1
    rng = (d["high"] - d["low"]).replace(0, np.nan)
    vol_ok = d["volume"] >= 1.8 * vmed
    buy = (ret >= 0.0025) & vol_ok & ((d["close"] - d["low"]) / rng >= 0.8)
    sell = (ret <= -0.0025) & vol_ok & ((d["high"] - d["close"]) / rng >= 0.8)
    mask = in_main_window(d) & (buy | sell)
    s = d[mask]
    return pd.DataFrame({"idx": s.index, "dir": np.where(buy[mask], 1, -1),
                         "stop_bps": 0.6 * s["atr_bps"].values, "rr": 2.0,
                         "target": s["minute"].values + 5 * 4, "cap": MAIN_LAST})


def h3_laggard(d, ctx):
    market = ctx["mkt_r5"].reindex(d["ts"]).values
    lag = d["r5"].values - market
    buy = (market >= 0.003) & (lag <= -0.0012)
    sell = (market <= -0.003) & (lag >= 0.0012)
    mask = in_main_window(d).values & (buy | sell)
    s = d[mask]
    return pd.DataFrame({"idx": s.index, "dir": np.where(buy[mask], 1, -1),
                         "stop_bps": 0.6 * s["atr_bps"].values, "rr": 1.5,
                         "target": s["minute"].values + 5 * 3, "cap": MAIN_LAST})


def h4_residual(d, ctx):
    market = ctx["mkt_r15"].reindex(d["ts"]).values
    med = ctx["med_resid15"].reindex(d["ts"]).values
    mad = ctx["mad_resid15"].reindex(d["ts"]).values
    resid = d["r15"].values - market
    with np.errstate(invalid="ignore", divide="ignore"):
        z = (resid - med) / np.where(mad > 0, mad, np.nan)
    sig = (np.abs(z) >= 2.5) & (np.abs(resid) >= 0.0025) & (np.abs(market) < 0.004)
    mask = in_main_window(d).values & sig
    s = d[mask]
    return pd.DataFrame({"idx": s.index, "dir": -np.sign(z[mask]).astype(int),
                         "stop_bps": 0.75 * s["atr_bps"].values, "rr": 1.0,
                         "target": s["minute"].values + 5 * 4, "cap": MAIN_LAST})


def h5_two_day(d, ctx):
    wd = d[d["weekday"] < 5]
    daily = wd.groupby("date").agg(H=("high", "max"), L=("low", "min"), C=("close", "last"))
    daily["H2"] = daily["H"].shift(1).rolling(2).max()
    daily["L2"] = daily["L"].shift(1).rolling(2).min()
    cp = daily["C"].shift()
    tr = np.maximum.reduce([daily["H"] - daily["L"], (daily["H"] - cp).abs(), (daily["L"] - cp).abs()])
    daily["buf"] = 0.1 * pd.Series(tr, index=daily.index).rolling(14).mean().shift(1)
    b = wd[wd["minute"].between(10 * 60, 14 * 60 + 55)].copy()
    b = b.join(daily[["H2", "L2", "buf"]], on="date")
    up = b["close"] > b["H2"] + b["buf"]
    dn = b["close"] < b["L2"] - b["buf"]
    b = b[up | dn].assign(dir=np.where(up[up | dn], 1, -1))
    b = b[~b.index.duplicated()].groupby("date").head(1)  # первый сигнал за дату
    return pd.DataFrame({"idx": b.index, "dir": b["dir"].values,
                         "stop_bps": 1.5 * b["atr_bps"].values, "rr": 2.0,
                         "target": MAIN_LAST, "cap": MAIN_LAST})


HYPOTHESES = [
    ("H1", "продолжение утреннего гэпа", h1_gap),
    ("H2", "всплеск объёма", h2_volume),
    ("H3", "догоняющая бумага", h3_laggard),
    ("H4", "разворот остатка", h4_residual),
    ("H5", "пробой двухдневного диапазона", h5_two_day),
]


def build_context(frames):
    for d in frames.values():
        d["r5"] = d["close"].values / lagged_close(d, 5) - 1
        d["r15"] = d["close"].values / lagged_close(d, 15) - 1
    _, mkt_r5 = core_market(frames, "r5")
    wide15, mkt_r15 = core_market(frames, "r15")
    resid = wide15.sub(mkt_r15, axis=0)
    med = resid.median(axis=1)
    mad = resid.sub(med, axis=0).abs().median(axis=1)
    return {"mkt_r5": mkt_r5, "mkt_r15": mkt_r15, "med_resid15": med, "mad_resid15": mad}


# ---------------------------------------------------------------- исполнение

def exit_index(date, minute, idx, target):
    """Последний бар той же даты, начинающийся не позже target. None, если входного бара нет."""
    n = len(date)
    j = idx + 1
    if j >= n or date[j] != date[idx] or minute[j] > target:
        return None
    while j + 1 < n and date[j + 1] == date[idx] and minute[j + 1] <= target:
        j += 1
    return j


def simulate(o, h, l, c, idx, ex, dirn, stop_bps, rr):
    """Сделка в модели position.IntrabarPath: вход по open idx+1, стоп не уже MIN_STOP_BPS."""
    entry = o[idx + 1]
    s = max(stop_bps, MIN_STOP_BPS)
    sl = entry * (1 - dirn * s / 1e4)
    tp = entry * (1 + dirn * s * rr / 1e4)
    for j in range(idx + 1, ex + 1):
        if j > idx + 1:  # open бара — реальная цена: стоп по худшей, тейк по уровню
            if (o[j] - sl) * dirn <= 0:
                return dirn * (o[j] / entry - 1) * 1e4, s
            if (o[j] - tp) * dirn >= 0:
                return dirn * (tp / entry - 1) * 1e4, s
        adverse, favorable = (l[j], h[j]) if dirn > 0 else (h[j], l[j])
        if (adverse - sl) * dirn <= 0:
            return dirn * (sl / entry - 1) * 1e4, s
        if (favorable - tp) * dirn >= 0:
            return dirn * (tp / entry - 1) * 1e4, s
    return dirn * (c[ex] / entry - 1) * 1e4, s


def run_trades(frames, tickers, fn, ctx, start, end):
    rows = []
    for t in tickers:
        d = frames[t]
        sig = fn(d, ctx)
        if sig.empty:
            continue
        sig = sig.dropna(subset=["stop_bps"]).sort_values("idx")
        date, minute = d["date"].values, d["minute"].values
        o, h, l, c = (d[k].values for k in ("open", "high", "low", "close"))
        busy_until = -1
        for r in sig.itertuples(index=False):
            idx = int(r.idx)
            if idx < busy_until or not (start <= date[idx] <= end):
                continue
            ex = exit_index(date, minute, idx, min(r.target, r.cap))
            if ex is None:
                continue
            busy_until = ex
            fwd = r.dir * (c[ex] / o[idx + 1] - 1) * 1e4
            fwd_close = r.dir * (c[ex] / c[idx] - 1) * 1e4
            sim, stop = simulate(o, h, l, c, idx, ex, r.dir, r.stop_bps, r.rr)
            rows.append({"ticker": t, "date": date[idx], "minute": int(minute[idx]), "dir": int(r.dir),
                         "rule_stop_bps": r.stop_bps, "stop_bps": stop, "rr": r.rr,
                         "fwd_bps": fwd, "fwd_close_bps": fwd_close, "sim_bps": sim})
    return pd.DataFrame(rows)


# ---------------------------------------------------------------- статистика

def cluster_t(x, dates):
    """t среднего с кластеризацией по дню."""
    if len(x) < 2:
        return float("nan")
    mu = x.mean()
    g = pd.Series(x - mu).groupby(np.asarray(dates)).sum()
    se = math.sqrt((g ** 2).sum()) / len(x)
    return mu / se if se > 0 else float("nan")


def one_sided_p(t):
    return 0.5 * math.erfc(t / math.sqrt(2)) if not math.isnan(t) else float("nan")


def block_of(dates, start, end):
    span = (end - start).days + 1
    k = np.array([(d - start).days for d in dates]) * N_BLOCKS // span
    return np.clip(k, 0, N_BLOCKS - 1)


def summarize(tr, start, end):
    c1, c2 = cost_bps(SLIPPAGE_BPS[0]), cost_bps(SLIPPAGE_BPS[1])
    fwd_net = tr["fwd_bps"] - c1
    r1 = (tr["sim_bps"] - c1) / tr["stop_bps"]
    r2 = (tr["sim_bps"] - c2) / tr["stop_bps"]
    net_rub = tr["sim_bps"] - c1
    blocks = block_of(tr["date"], start, end)
    bf = fwd_net.groupby(blocks).mean().reindex(range(N_BLOCKS))
    br = r1.groupby(blocks).mean().reindex(range(N_BLOCKS))
    t = cluster_t(fwd_net.values, tr["date"].values)
    loss = -net_rub[net_rub < 0].sum()
    return {
        "n": len(tr),
        "fwd_net": fwd_net.mean(),
        "fwd_close_net": (tr["fwd_close_bps"] - c1).mean(),
        "t": t, "p": one_sided_p(t),
        "blocks_fwd_pos": int((bf > 0).sum()),
        "exp_r1": r1.mean(), "exp_r2": r2.mean(),
        "pf": net_rub[net_rub > 0].sum() / loss if loss > 0 else float("nan"),
        "wr": (net_rub > 0).mean(),
        "blocks_r_pos": int((br > 0).sum()), "worst_block_r": br.min(),
        "block_n": np.bincount(blocks, minlength=N_BLOCKS).tolist(),
        "median_stop": tr["stop_bps"].median(),
        "floored": (tr["rule_stop_bps"] < MIN_STOP_BPS).mean(),
    }


def holm(pvals, alpha=0.05):
    order = sorted(range(len(pvals)), key=lambda i: (math.isnan(pvals[i]), pvals[i]))
    passed = [False] * len(pvals)
    m = len(pvals)
    for rank, i in enumerate(order):
        if math.isnan(pvals[i]) or pvals[i] > alpha / (m - rank):
            break
        passed[i] = True
    return passed


def verdict(core, check, holm_ok):
    if core["n"] < 200 or not core["fwd_net"] > 0 or core["blocks_fwd_pos"] < 4:
        return "БРОСИТЬ"
    candidate = (core["exp_r1"] >= 0.08 and core["blocks_r_pos"] >= 4 and core["worst_block_r"] >= -0.10
                 and core["exp_r2"] >= 0 and holm_ok
                 and check["n"] > 0 and check["exp_r1"] > 0 and check["fwd_net"] > 0)
    return "КАНДИДАТ" if candidate else "не опровергнута, не доказана"


def fmt(s):
    return (f"n={s['n']:5d}  fwd_net={s['fwd_net']:+6.2f} б.п. (close-вход {s['fwd_close_net']:+6.2f})  "
            f"t={s['t']:+5.2f}  блоков+ {s['blocks_fwd_pos']}/6\n"
            f"           exp_R@1={s['exp_r1']:+.3f}  exp_R@2={s['exp_r2']:+.3f}  PF={s['pf']:.2f}  "
            f"WR={s['wr']:.1%}  блоков R+ {s['blocks_r_pos']}/6, худший {s['worst_block_r']:+.3f}\n"
            f"           стоп медиана {s['median_stop']:.1f} б.п., подняты до {MIN_STOP_BPS:.0f}: {s['floored']:.0%}; "
            f"сделок по блокам {s['block_n']}")


def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--history-dir", default="data/history")
    ap.add_argument("--dry-run", action="store_true", help="только число сделок, без доходности")
    ap.add_argument("--trades", help="CSV со всеми сделками скрининга")
    args = ap.parse_args()

    frames = load(args.history_dir)
    ctx = build_context(frames)

    results, all_trades = [], []
    for code, name, fn in HYPOTHESES:
        per_group = {}
        for g, tickers in GROUPS.items():
            start, end = PERIODS[g]
            if code == "H1":
                start = max(start, MORNING_FROM)
            tr = run_trades(frames, tickers, fn, ctx, start, end)
            per_group[g] = (tr, start, end)
            if args.trades and not tr.empty:
                all_trades.append(tr.assign(hypothesis=code, group=g))
        results.append((code, name, per_group))

    if args.dry_run:
        for code, name, per_group in results:
            counts = ", ".join(f"{g}: {len(tr)}" for g, (tr, _, _) in per_group.items())
            print(f"{code} {name}: {counts}")
        return

    stats = []
    for code, name, per_group in results:
        s = {g: (summarize(tr, st, en) if len(tr) else None) for g, (tr, st, en) in per_group.items()}
        stats.append((code, name, s))
    core_p = [s["ядро-10"]["p"] if s["ядро-10"] else float("nan") for _, _, s in stats]
    holm_ok = holm(core_p)

    print(f"Скрининг по 0008. Издержки {cost_bps(SLIPPAGE_BPS[0]):.1f} б.п. на круг "
          f"(стресс {cost_bps(SLIPPAGE_BPS[1]):.1f}). Вход — open бара после сигнала.\n")
    for (code, name, s), hk in zip(stats, holm_ok):
        print(f"{code} — {name}")
        for g in GROUPS:
            print(f"  {g:12s}" + (fmt(s[g]) if s[g] else "нет сделок"))
        core, check = s["ядро-10"], s["проверка-15"]
        if core:
            empty = {"n": 0, "exp_r1": float("nan"), "fwd_net": float("nan")}
            print(f"  Holm: {'проходит' if hk else 'не проходит'} (p={core['p']:.4f})")
            print(f"  ВЕРДИКТ: {verdict(core, check or empty, hk)}\n")

    if args.trades and all_trades:
        pd.concat(all_trades).to_csv(args.trades, index=False)
        print(f"сделки: {args.trades}")


if __name__ == "__main__":
    main()
