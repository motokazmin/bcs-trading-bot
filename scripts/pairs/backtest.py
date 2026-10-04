"""0020: внутридневные пары «обычка / преф» — подбор и хвост по регистрации.

python3 scripts/pairs/backtest.py train            # сетка (N, k) на подборе, выбор набора → results/pairs/chosen.json
python3 scripts/pairs/backtest.py holdout          # один прогон выбранного набора на хвосте + диагностика

Правило, издержки и критерий — docs/analysis/0020-pref-common-pairs.md. Вход и выход — open
следующего бара по обеим ногам, EOD — close бара, кончающегося в 18:40.
"""
import json
import os
import sys

import numpy as np
import pandas as pd

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from describe import ROOT, m5_pair  # noqa: E402

OUT = os.path.join(ROOT, "results", "pairs")
MAIN = [("TATN", "TATNP"), ("RTKM", "RTKMP"), ("MTLR", "MTLRP"), ("SNGS", "SNGSP")]
CONTROL = [("SBER", "SBERP")]
TRAIN = ("2022-10-05", "2025-03-31")
HOLDOUT = ("2025-04-01", "2026-09-30")
GRID_N = (12, 24, 48)
GRID_K = (1.5, 2.0, 3.0)
MIN_TRADES = 100
COMMISSION_BPS = 0.8
# Проскальзывание на ногу = половина спреда Ролла на подборе, не меньше 1 б.п. (зафиксировано в 0020).
SLIP_BPS = {"TATN": 3.3, "TATNP": 2.9, "RTKM": 4.0, "RTKMP": 6.3, "MTLR": 5.0, "MTLRP": 6.1,
            "SNGS": 4.6, "SNGSP": 3.5, "SBER": 2.4, "SBERP": 2.4}
WARMUP = 6           # баров сегодняшнего дня до первого сигнала
LAST_ENTRY = "18:00"  # бар входа должен начинаться раньше


def pair_cost(c, p, slip_mult=1.0):
    return 2 * slip_mult * (SLIP_BPS[c] + SLIP_BPS[p]) + 4 * COMMISSION_BPS


_cache = {}


def load(c, p, period):
    key = (c, p)
    if key not in _cache:
        _cache[key] = m5_pair(c, p)
    j = _cache[key]
    return j[(j.index >= period[0]) & (j.index < pd.Timestamp(period[1]) + pd.Timedelta(days=1))]


def leg_ret(entry, exit_, side, slip):
    """Доходность ноги в б.п. с проскальзыванием против позиции: side +1 лонг, −1 шорт."""
    if side > 0:
        return (exit_ * (1 - slip) / (entry * (1 + slip)) - 1) * 1e4
    return (1 - exit_ * (1 + slip) / (entry * (1 - slip))) * 1e4


def run_pair(c, p, period, n_anchor, k, slip_mult=1.0, fill="open_next", slip_override=None):
    j = load(c, p, period)
    sc = (slip_override if slip_override is not None else SLIP_BPS[c] * slip_mult) / 1e4
    sp = (slip_override if slip_override is not None else SLIP_BPS[p] * slip_mult) / 1e4
    cost = pair_cost(c, p)  # порог всегда от зарегистрированных издержек, не от стресса
    thr = k * cost
    trades = []
    for day, g in j.groupby("day", sort=True):
        s = g["s"].to_numpy()
        oc, op = g["open_c"].to_numpy(), g["open_p"].to_numpy()
        cc, cp = g["close_c"].to_numpy(), g["close_p"].to_numpy()
        traded = (g["volume_c"].to_numpy() > 0) & (g["volume_p"].to_numpy() > 0)
        hhmm = g.index.strftime("%H:%M").to_numpy()
        n = len(s)
        pos = None  # (side спреда, индекс бара входа, цены входа c, p)
        t = 0
        while t < n:
            if t >= WARMUP:
                a = s[max(0, t - n_anchor):t].mean()
                d = s[t] - a
            else:
                d = None
            last = t == n - 1
            if pos is not None:
                side = pos[0]
                if last:
                    trades.append(close_trade(day, c, p, pos, cc[t], cp[t], sc, sp, "eod"))
                    pos = None
                elif d is not None and side * d <= 0:
                    xc, xp = (oc[t + 1], op[t + 1]) if fill == "open_next" else (cc[t], cp[t])
                    trades.append(close_trade(day, c, p, pos, xc, xp, sc, sp, "anchor"))
                    pos = None
                    t += 1 if fill == "open_next" else 0
                    if fill == "open_next":
                        continue  # бар выхода не может дать вход по своему же open
            elif d is not None and not last and traded[t] and abs(d) >= thr and hhmm[t + 1] < LAST_ENTRY:
                side = 1 if d > 0 else -1  # +1: преф дорог — шорт префа, лонг обычки
                if fill == "open_next":
                    pos = (side, t + 1, oc[t + 1], op[t + 1])
                    t += 1
                    continue  # на баре входа выход проверяется уже по его close
                pos = (side, t, cc[t], cp[t])
            t += 1
    return trades


def close_trade(day, c, p, pos, xc, xp, sc, sp, reason):
    side, _, ec, ep = pos
    # side +1: лонг обычки, шорт префа.
    r = leg_ret(ec, xc, side, sc) + leg_ret(ep, xp, -side, sp) - 4 * COMMISSION_BPS
    return {"day": day, "pair": c, "side": side, "pnl_bps": r, "reason": reason}


def stats(trades):
    if not trades:
        return 0, np.nan, np.nan
    df = pd.DataFrame(trades)
    n = len(df)
    m = df["pnl_bps"].mean()
    g = df.groupby("day")["pnl_bps"].agg(["sum", "count"])
    se = np.sqrt(((g["sum"] - m * g["count"]) ** 2).sum()) / n
    return n, m, m / se if se > 0 else np.nan


def pool(pairs, period, n_anchor, k, **kw):
    per = {c: run_pair(c, p, period, n_anchor, k, **kw) for c, p in pairs}
    return per, [t for v in per.values() for t in v]


def fmt(per, allt):
    n, m, t = stats(allt)
    parts = " ".join(f"{c} {stats(v)[1]:+6.1f}({len(v)})" for c, v in per.items())
    return f"n {n:5d}  ср. {m:+6.2f} б.п.  t {t:+5.2f}  | {parts}"


def train():
    os.makedirs(OUT, exist_ok=True)
    print(f"Подбор {TRAIN[0]} → {TRAIN[1]}; издержки круга: "
          + ", ".join(f"{c} {pair_cost(c, p):.1f}" for c, p in MAIN + CONTROL))
    best = None
    for n_anchor in GRID_N:
        for k in GRID_K:
            per, allt = pool(MAIN, TRAIN, n_anchor, k)
            n, m, t = stats(allt)
            print(f"N {n_anchor:3d} k {k:3.1f}  {fmt(per, allt)}")
            if n >= MIN_TRADES and (best is None or t > best[2]):
                best = (n_anchor, k, t)
    if best is None:
        print("ни один набор не набрал сделок — хвост не запускается")
        return
    chosen = {"N": best[0], "k": best[1], "train_t": round(best[2], 3)}
    with open(os.path.join(OUT, "chosen.json"), "w") as f:
        json.dump(chosen, f)
    print("выбран:", chosen)
    per, allt = pool(CONTROL, TRAIN, best[0], best[1])
    print(f"контроль SBER/SBERP на подборе: {fmt(per, allt)}")


def holdout():
    with open(os.path.join(OUT, "chosen.json")) as f:
        ch = json.load(f)
    nA, k = ch["N"], ch["k"]
    print(f"Хвост {HOLDOUT[0]} → {HOLDOUT[1]}, набор N {nA}, k {k} (t на подборе {ch['train_t']})")
    per, allt = pool(MAIN, HOLDOUT, nA, k)
    n, m, t = stats(allt)
    pos_pairs = sum(1 for v in per.values() if stats(v)[1] > 0)
    print("основной набор:", fmt(per, allt))
    _, allt15 = pool(MAIN, HOLDOUT, nA, k, slip_mult=1.5)
    m15 = stats(allt15)[1]
    c1, c2, c3 = (m > 0 and t >= 2), pos_pairs >= 3, m15 > 0
    print(f"критерий: 1) ср. > 0 и t ≥ 2: {'да' if c1 else 'нет'};  2) пар в плюсе {pos_pairs}/4: "
          f"{'да' if c2 else 'нет'};  3) при проскальзывании ×1.5 ср. {m15:+.2f}: {'да' if c3 else 'нет'}")
    print("ИТОГ:", "преимущество есть" if (c1 and c2 and c3) else "преимущества нет")

    print("\nДиагностика (не критерий):")
    per_c, allt_c = pool(CONTROL, HOLDOUT, nA, k)
    print("  контроль SBER/SBERP:", fmt(per_c, allt_c))
    per_x, allt_x = pool(MAIN, HOLDOUT, nA, k, fill="close_signal")
    print("  фил по close сигнального бара:", fmt(per_x, allt_x))
    per_1, allt_1 = pool(MAIN, HOLDOUT, nA, k, slip_override=1.0)
    print("  проскальзывание 1 б.п.:", fmt(per_1, allt_1))
    df = pd.DataFrame(allt)
    print("  причины выхода:", df["reason"].value_counts().to_dict())
    years = (pd.Timestamp(HOLDOUT[1]) - pd.Timestamp(HOLDOUT[0])).days / 365.25
    for c, v in per.items():
        tot = sum(x["pnl_bps"] for x in v)
        # Капитал пары = 2·N₁; годовая доходность на капитал, %.
        print(f"  {c}: сделок/день {len(v) / (years * 252):.2f}, годовая на капитал {tot / 2 / 1e2 / years:+.1f}%")
    tot = sum(x["pnl_bps"] for x in allt)
    print(f"  четыре пары, капитал 4·2·N₁: годовая {tot / 8 / 1e2 / years:+.1f}%")
    j = {c: load(c, p, HOLDOUT) for c, p in MAIN}
    for c, p in MAIN:
        turn = j[c].groupby("day")[["volume_c", "volume_p"]].sum().median().min() / 1e6
        print(f"  {c}: оборот менее ликвидной ноги, медиана {turn:.0f} млн ₽/день → 1% = {turn * 10:.0f} тыс ₽")


if __name__ == "__main__":
    {"train": train, "holdout": holdout}[sys.argv[1]]()
