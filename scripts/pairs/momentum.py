"""0022: относительный моментум на фьючерсах на акции — 12−1, удержание месяц, крайние трети.

python3 scripts/pairs/momentum.py            # основной прогон, критерий и диагностика

Правило, издержки и критерий — docs/analysis/0022-futures-cross-sectional-momentum.md.
Ряды и издержки — общие с 0021 (scripts/pairs/distance.py).
"""
import os
import sys

import numpy as np
import pandas as pd

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from distance import END, FEE_BPS, MIN_COVERAGE, MIN_VALUE, SUBPERIODS, load, nw_t, slip_bps  # noqa: E402

FIRST = "2015-10"
WINDOW = 252  # дней для вселенной и оборота


def month_ends(index):
    s = pd.Series(index, index=index)
    return s.groupby(index.to_period("M")).max()


def run(ret, val, roll, lookback=12, cost_mult=1.0, rng=None):
    """Дневной P&L на капитал 1 и журнал ребалансировок."""
    idx = ret.index
    pos_of = {d: i for i, d in enumerate(idx)}
    ends = month_ends(idx)
    R = ret.fillna(0).to_numpy()
    RL = roll.to_numpy().astype(bool)
    assets = list(ret.columns)
    w = np.zeros(len(assets))  # текущая стоимость позиции (лонг > 0, шорт < 0) на капитал 1
    cost = np.zeros(len(assets))
    pnl = np.zeros(len(idx))
    ew = np.full(len(idx), np.nan)  # равновзвешенная вселенная (бенчмарк лонг/шорт-сторон)
    long_pnl, short_pnl = np.zeros(len(idx)), np.zeros(len(idx))
    log = []
    target, exec_day, uni_mask = None, None, None
    rebal = {}
    periods = list(ends.index)
    for k, per in enumerate(periods):
        if per < pd.Period(FIRST) - 1 or k < lookback:
            continue
        i = pos_of[ends[per]]
        if i + 1 >= len(idx) or i < WINDOW:
            continue
        rebal[i + 1] = (i, k)

    for t in range(len(idx)):
        # 1. доходность дня по вчерашним позициям
        r = R[t]
        g = w * r
        pnl[t] += g.sum()
        long_pnl[t] += g[w > 0].sum()
        short_pnl[t] += g[w < 0].sum()
        if uni_mask is not None:
            ew[t] = r[uni_mask].mean()
        w = w * (1 + r)
        # перекладка контракта у открытой ноги — две стороны
        rolled = RL[t] & (w != 0)
        pnl[t] -= (2 * cost * np.abs(w))[rolled].sum()
        # 2. ребалансировка по сигналу на конец прошлого месяца — по расчётной цене сегодня
        if t in rebal:
            i, k = rebal[t]
            lo = WINDOW
            win = slice(i - lo + 1, i + 1)
            cover = ret.iloc[win].notna().mean().to_numpy()
            medv = val.iloc[win].median().to_numpy()
            ok = (cover >= MIN_COVERAGE) & (medv >= MIN_VALUE)
            # сигнал: доходность с конца месяца m−lookback по конец месяца m−1
            a = pos_of[ends[periods[k - lookback]]]
            b = pos_of[ends[periods[k - 1]]]
            sig = np.prod(1 + R[a + 1:b + 1], axis=0) - 1
            names = np.where(ok)[0]
            new = np.zeros(len(assets))
            if len(names) >= 2:
                s = sig[names] if rng is None else rng.permutation(len(names)).astype(float)
                order = names[np.argsort(s)]
                q = max(1, len(names) // 3)
                new[order[-q:]] = 1.0 / q
                new[order[:q]] = -1.0 / q
            c_now = cost_mult * (FEE_BPS + np.array([slip_bps(v) if np.isfinite(v) else 10.0 for v in medv])) / 1e4
            trade = np.abs(new - w)
            c = (trade * np.where(trade > 0, c_now, 0)).sum()
            pnl[t] -= c
            cost = c_now
            w = new
            uni_mask = ok
            log.append({"date": idx[t], "n": int(ok.sum()), "q": int(max(1, ok.sum() // 3)) if ok.sum() >= 2 else 0,
                        "turnover": trade.sum(), "cost": c,
                        "long": [assets[j] for j in np.where(new > 0)[0]],
                        "short": [assets[j] for j in np.where(new < 0)[0]]})
    daily = pd.DataFrame({"pnl": pnl, "long": long_pnl, "short": short_pnl, "ew": ew}, index=idx)
    daily = daily[daily.index >= pd.Period(FIRST).start_time]
    return daily, pd.DataFrame(log)


def monthly(daily, col="pnl"):
    return daily[col].groupby(daily.index.to_period("M")).sum()


def summary(m_ret, label):
    m = m_ret * 1e4
    cum = m_ret.cumsum()
    dd = (cum - cum.cummax()).min() * 100
    sharpe = m_ret.mean() / m_ret.std() * np.sqrt(12)
    subs = [m[(m.index >= pd.Period(a)) & (m.index <= pd.Period(b))].mean() for a, b in SUBPERIODS]
    print(f"{label:30} мес. {len(m):3d}  ср. {m.mean():+6.1f} б.п./мес (t_NW {nw_t(m, 3):+5.2f})  "
          f"год {m_ret.mean() * 1200:+5.1f}%  Sharpe {sharpe:+5.2f}  просадка {dd:+5.1f}%  "
          f"| подпериоды {' / '.join(f'{x:+6.1f}' for x in subs)}")
    return m, subs


def main():
    ret, val, roll = load()
    ret = ret[ret.index <= END]
    daily, log = run(ret, val, roll)
    m, subs = summary(monthly(daily), "ОСНОВНОЙ 12−1 (×1 издержки)")
    m2, _ = summary(monthly(run(ret, val, roll, cost_mult=2.0)[0]), "издержки ×2")
    c1 = m.mean() > 0 and nw_t(m, 3) >= 2
    c2 = sum(x > 0 for x in subs) >= 2
    c3 = m2.mean() > 0
    print(f"\nкритерий: 1) ср. > 0 и t ≥ 2: {'да' if c1 else 'нет'};  2) подпериодов в плюсе "
          f"{sum(x > 0 for x in subs)}/3: {'да' if c2 else 'нет'};  3) при издержках ×2: {'да' if c3 else 'нет'}")
    print("ИТОГ:", "преимущество есть" if (c1 and c2 and c3) else "преимущества нет")

    print("\nДиагностика (не критерий):")
    ewm = monthly(daily, "ew")
    lm = monthly(daily, "long") - ewm
    sm = monthly(daily, "short") + ewm
    print(f"  лонг-сторона минус вселенная: {lm.mean() * 1e4:+.1f} б.п./мес (t {nw_t(lm * 1e4, 3):+.2f}); "
          f"шорт-сторона против вселенной: {sm.mean() * 1e4:+.1f} (t {nw_t(sm * 1e4, 3):+.2f})")
    print(f"  корреляция со вселенной: {np.corrcoef(monthly(daily), ewm)[0, 1]:+.2f}; "
          f"вселенная сама: {ewm.mean() * 1e4:+.1f} б.п./мес")
    print(f"  бумаг во вселенной: медиана {log.n.median():.0f} (мин {log.n.min()}, макс {log.n.max()}); "
          f"в трети {log.q.median():.0f}; оборот {log.turnover.mean():.2f} в месяц, издержки ребалансировки "
          f"{log.cost.mean() * 1e4:.1f} б.п./мес")
    for lb in (6, 3):
        summary(monthly(run(ret, val, roll, lookback=lb)[0]), f"устойчивость {lb}−1")
    nulls = [monthly(run(ret, val, roll, rng=np.random.default_rng(s))[0]).mean() * 1e4 for s in range(20)]
    print(f"  нулевая модель (случайный ранг, 20 зёрен): ср. {np.mean(nulls):+.1f} б.п./мес, "
          f"разброс {np.std(nulls):.1f}, лучшее {max(nulls):+.1f}")
    worst = monthly(daily).nsmallest(5) * 1e4
    print("  худшие месяцы, б.п.:", ", ".join(f"{p} {v:+.0f}" for p, v in worst.items()))
    by_year = monthly(daily).groupby(lambda p: p.year).sum() * 100
    print("  по годам, %:", ", ".join(f"{y} {v:+.1f}" for y, v in by_year.items()))


if __name__ == "__main__":
    main()
