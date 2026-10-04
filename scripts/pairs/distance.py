"""0021: парный арбитраж разных компаний на фьючерсах — метод расстояний (Gatev, Goetzmann, Rouwenhorst).

python3 scripts/pairs/distance.py            # основной прогон, критерий и диагностика

Правило, издержки и критерий — docs/analysis/0021-futures-pairs-distance.md. Ряды —
data/futures-d/continuous (scripts/pairs/futures_series.py). Подбора нет: каждое торговое окно
лежит после своего окна формирования.
"""
import glob
import itertools
import os

import numpy as np
import pandas as pd

ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
SRC = os.path.join(ROOT, "data", "futures-d", "continuous")
FORM, TRADE = 252, 126
N_PAIRS = 10
ENTRY_SIGMA = 2.0
MIN_COVERAGE = 0.95
MIN_VALUE = 50e6
FEE_BPS = 1.0
FIRST_TRADE = "2015-10-01"
END = "2026-09-30"
SUBPERIODS = [("2015-10", "2019-12"), ("2020-01", "2022-12"), ("2023-01", "2026-09")]
SAME_ISSUER = {frozenset(p) for p in (("SBRF", "SBPR"), ("SNGR", "SNGP"), ("TATN", "TATP"))}


def slip_bps(value):
    if value >= 1e9:
        return 2.0
    if value >= 100e6:
        return 5.0
    return 10.0


def load():
    ret, val, roll = {}, {}, {}
    for p in glob.glob(os.path.join(SRC, "*.csv")):
        a = os.path.basename(p)[:-4]
        f = pd.read_csv(p, parse_dates=["date"]).set_index("date")
        ret[a], val[a], roll[a] = f["ret"], f["value"], f["roll"].astype(bool)
    ret, val, roll = pd.DataFrame(ret), pd.DataFrame(val), pd.DataFrame(roll).fillna(False)
    ret = ret[ret.index <= END]
    return ret, val.reindex(ret.index), roll.reindex(ret.index).fillna(False)


def select(ret, val, f0, f1, rng=None):
    """Вселенная и 10 пар по окну формирования [f0, f1). rng — случайные пары (нулевая модель)."""
    r = ret.iloc[f0:f1]
    cover = r.notna().mean()
    medv = val.iloc[f0:f1].median()
    uni = [a for a in ret.columns if cover[a] >= MIN_COVERAGE and medv[a] >= MIN_VALUE]
    if len(uni) < 2:
        return uni, [], medv
    P = (1 + r[uni].fillna(0)).cumprod()
    pairs = list(itertools.combinations(sorted(uni), 2))
    if rng is not None:
        idx = rng.choice(len(pairs), size=min(N_PAIRS, len(pairs)), replace=False)
        chosen = [pairs[i] for i in idx]
    else:
        dist = {(a, b): float(((P[a] - P[b]) ** 2).sum()) for a, b in pairs}
        chosen = sorted(dist, key=dist.get)[:N_PAIRS]
    out = [(a, b, float((P[a] - P[b]).std())) for a, b in chosen]
    return uni, out, medv


def trade_pair(a, b, sigma, r, rl, cost_a, cost_b, delay=1):
    """Торговое окно одной пары. Возвращает дневной P&L (на 1 ₽ в каждой ноге при входе) и сделки.

    Решение — по расчётной цене дня t; исполнение — по расчётной цене дня t + delay.
    Позиция зарабатывает доходность дней после исполнения.
    """
    ra, rb = r[a].fillna(0).to_numpy(), r[b].fillna(0).to_numpy()
    rla, rlb = rl[a].to_numpy(), rl[b].to_numpy()
    n = len(ra)
    spread = np.cumprod(1 + ra) - np.cumprod(1 + rb)
    pnl = np.zeros(n)
    trades = []
    st = {"pos": 0, "L": 0.0, "S": 0.0, "tr": None}  # pos +1: лонг a, шорт b (a отстала)

    def legs():
        return (cost_a, cost_b, rla, rlb) if st["pos"] > 0 else (cost_b, cost_a, rlb, rla)

    def charge(t, c):
        pnl[t] -= c
        st["tr"]["pnl"] -= c
        st["tr"]["cost"] += c

    def execute(t, action, reason=None):
        if action == "close":
            cl, cs, _, _ = legs()
            charge(t, cl * st["L"] + cs * st["S"])
            tr = st["tr"]
            tr.update(close=t, reason=reason)
            trades.append(tr)
            st.update(pos=0, tr=None)
        else:
            st.update(pos=action, L=1.0, S=1.0,
                      tr={"a": a, "b": b, "open": t, "pnl": 0.0, "cost": 0.0,
                          "same_issuer": frozenset((a, b)) in SAME_ISSUER})
            charge(t, cost_a + cost_b)

    pending = None  # (день исполнения, действие)
    for t in range(n):
        if st["pos"] != 0:
            # доходность дня по ногам и перекладки сегодня (две стороны по стоимости ноги)
            rl_, rs_ = (ra[t], rb[t]) if st["pos"] > 0 else (rb[t], ra[t])
            g = st["L"] * rl_ - st["S"] * rs_
            pnl[t] += g
            st["tr"]["pnl"] += g
            st["L"] *= 1 + rl_
            st["S"] *= 1 + rs_
            cl, cs, rolll, rolls = legs()
            if rolll[t]:
                charge(t, 2 * cl * st["L"])
            if rolls[t]:
                charge(t, 2 * cs * st["S"])
        if pending is not None and pending[0] == t:
            execute(t, pending[1], "схождение")
            pending = None
        if pending is not None:
            continue
        if t == n - 1:
            if st["pos"] != 0:
                execute(t, "close", "конец окна")
            continue
        action = None
        if st["pos"] != 0 and spread[t] * st["pos"] >= 0:
            action = "close"  # pos +1 открыт при spread < 0 — схождение, когда spread ≥ 0
        elif st["pos"] == 0 and abs(spread[t]) > ENTRY_SIGMA * sigma and t + delay < n - 1:
            action = 1 if spread[t] < 0 else -1
        if action is not None:
            if delay == 0:
                execute(t, action, "схождение")
            else:
                pending = (t + delay, action)
    return pnl, trades


def run(ret, val, roll, cost_mult=1.0, rng=None, delay=1):
    dates = ret.index
    starts = []
    for m in pd.period_range(FIRST_TRADE[:7], END[:7], freq="M"):
        i = dates.searchsorted(m.start_time)
        if i < len(dates) and i >= FORM and dates[i] <= pd.Timestamp(END):
            starts.append(i)
    daily = []  # (портфель, Series дневной доходности на задействованный капитал)
    trades = []
    for s in starts:
        uni, pairs, medv = select(ret, val, s - FORM, s, rng)
        e = min(s + TRADE, len(dates))
        r, rl = ret.iloc[s:e], roll.iloc[s:e]
        port = np.zeros(e - s)
        for a, b, sigma in pairs:
            ca = cost_mult * (FEE_BPS + slip_bps(medv[a])) / 1e4
            cb = cost_mult * (FEE_BPS + slip_bps(medv[b])) / 1e4
            pnl, tr = trade_pair(a, b, sigma, r, rl, ca, cb, delay)
            port += pnl / N_PAIRS  # капитал — все 10 пар, открыты или нет
            for x in tr:
                x["start"] = dates[s]
            trades += tr
        daily.append(pd.Series(port, index=dates[s:e]))
    # Месяц стратегии — среднее по живым в нём портфелям (до 6 перекрывающихся).
    monthly = pd.concat([d.groupby(d.index.to_period("M")).sum() for d in daily], axis=1).mean(axis=1)
    return monthly, pd.DataFrame(trades)


def nw_t(x, lag=6):
    x = np.asarray(x, dtype=float)
    x = x[np.isfinite(x)]
    n, m = len(x), x.mean()
    u = x - m
    v = (u @ u) / n
    for k in range(1, lag + 1):
        v += 2 * (1 - k / (lag + 1)) * (u[k:] @ u[:-k]) / n
    return m / np.sqrt(v / n)


def summary(monthly, label):
    m = monthly * 1e4  # б.п. в месяц на задействованный капитал
    cum = monthly.cumsum()
    dd = (cum - cum.cummax()).min() * 100
    sharpe = monthly.mean() / monthly.std() * np.sqrt(12)
    subs = []
    for a, b in SUBPERIODS:
        x = m[(m.index >= pd.Period(a)) & (m.index <= pd.Period(b))]
        subs.append(f"{x.mean():+6.1f}")
    print(f"{label:34} мес. {len(m):3d}  ср. {m.mean():+6.1f} б.п./мес (t_NW {nw_t(m):+5.2f})  "
          f"год {monthly.mean() * 1200:+5.1f}%  Sharpe {sharpe:+5.2f}  просадка {dd:+5.1f}%  "
          f"| подпериоды {' / '.join(subs)}")
    return m


def main():
    ret, val, roll = load()
    print(f"Ряды: {ret.shape[1]} фьючерсов, {ret.index[0].date()} → {ret.index[-1].date()}\n")
    monthly, trades = run(ret, val, roll)
    m = summary(monthly, "ОСНОВНОЙ (×1 издержки)")
    m2 = summary(run(ret, val, roll, cost_mult=2.0)[0], "издержки ×2")
    c1 = m.mean() > 0 and nw_t(m) >= 2
    subs_pos = sum(m[(m.index >= pd.Period(a)) & (m.index <= pd.Period(b))].mean() > 0 for a, b in SUBPERIODS)
    c2, c3 = subs_pos >= 2, m2.mean() > 0
    print(f"\nкритерий: 1) ср. > 0 и t ≥ 2: {'да' if c1 else 'нет'};  2) подпериодов в плюсе {subs_pos}/3: "
          f"{'да' if c2 else 'нет'};  3) при издержках ×2: {'да' if c3 else 'нет'}")
    print("ИТОГ:", "преимущество есть" if (c1 and c2 and c3) else "преимущества нет")

    print("\nДиагностика (не критерий):")
    t = trades
    print(f"  сделок {len(t)}, ср. {t.pnl.mean() * 1e4:+.1f} б.п. (до издержек {(t.pnl + t.cost).mean() * 1e4:+.1f}), "
          f"в плюсе {(t.pnl > 0).mean():.0%}, закрыто концом окна {(t.reason == 'конец окна').mean():.0%}, "
          f"ср. длительность {(t.close - t.open).mean():.0f} дн.")
    for flag, g in t.groupby("same_issuer"):
        print(f"  {'один эмитент' if flag else 'разные компании'}: сделок {len(g)}, ср. {g.pnl.mean() * 1e4:+.1f} б.п., "
              f"до издержек {(g.pnl + g.cost).mean() * 1e4:+.1f}")
    top = t.groupby(t.a + "/" + t.b).pnl.agg(["count", "mean"]).sort_values("count", ascending=False).head(12)
    print("  самые частые пары (сделок, ср. б.п.):", ", ".join(f"{k} {int(v['count'])} {v['mean'] * 1e4:+.0f}"
                                                         for k, v in top.iterrows()))
    m0, t0 = run(ret, val, roll, delay=0)
    print(f"  исполнение в день сигнала (без ожидания): ср. {m0.mean() * 1e4:+.1f} б.п./мес, "
          f"сделка {t0.pnl.mean() * 1e4:+.1f} б.п.")
    nulls = []
    for seed in range(20):
        mm, _ = run(ret, val, roll, rng=np.random.default_rng(seed))
        nulls.append(mm.mean() * 1e4)
    print(f"  нулевая модель (случайные 10 пар, 20 зёрен): ср. {np.mean(nulls):+.1f} б.п./мес, "
          f"разброс зёрен {np.std(nulls):.1f}, лучшее {max(nulls):+.1f}")


if __name__ == "__main__":
    main()
