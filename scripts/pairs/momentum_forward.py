"""0023: проверка моментума вперёд на paper — журнал портфелей и счёт засчитанных месяцев.

PAIRS_END=$(date +%F) python3 scripts/pairs/momentum_forward.py signal [YYYY-MM]   # портфель месяца → журнал
PAIRS_END=$(date +%F) python3 scripts/pairs/momentum_forward.py score              # доходность и сверка с журналом
PAIRS_END=$(date +%F) python3 scripts/pairs/momentum_forward.py signal-dry         # тот же расчёт без записи (репетиция)

Правило — ровно 0022 (`momentum.target`), регистрация и критерий — docs/analysis/0023-momentum-forward-paper.md.
Журнал коммитится до расчётной цены первого торгового дня месяца: время коммита — улика, что портфель
выбран до исхода.
"""
import csv
import os
import sys
from datetime import date, datetime

import numpy as np
import pandas as pd

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from distance import ROOT, SRC, load, nw_t  # noqa: E402
from momentum import month_ends, monthly, run, target  # noqa: E402

JOURNAL = os.path.join(ROOT, "docs", "analysis", "0023-journal.csv")
TRIAL = pd.Period("2026-10")      # исполнение прошло до регистрации — прогон процедуры, не в критерий
FIRST_COUNTED = pd.Period("2026-11")
CAPITAL = 1_000_000               # ₽ на сторону — для целых контрактов (диагностика округления)
GATES = {12: -172.0, 24: -76.0}   # стоп, если среднее засчитанных месяцев ниже (б.п./мес)
FIELDS = ["month", "status", "signal_date", "asset", "side", "weight", "secid", "settle", "contracts",
          "universe", "generated_at"]


def read_journal():
    if not os.path.exists(JOURNAL):
        return pd.DataFrame(columns=FIELDS)
    return pd.read_csv(JOURNAL, dtype={"month": str})


def trade_month(arg):
    if arg:
        return pd.Period(arg, "M")
    today = pd.Timestamp(date.today())
    # после последнего буднего дня месяца — портфель следующего месяца, иначе — текущего
    last_weekday = pd.bdate_range(today, today + pd.offsets.MonthEnd(0))[-1]
    return today.to_period("M") + (1 if today >= last_weekday else 0)


def signal(arg=None, dry=False):
    """Портфель месяца T в журнал. dry — без записи: ежедневная репетиция (momentum_health.sh) портфеля
    текущего месяца по закрытому прошлому — проверяет весь путь, кроме записи."""
    T = pd.Timestamp(date.today()).to_period("M") if dry else trade_month(arg)
    M = T - 1
    j = read_journal()
    if not dry and (j["month"] == str(T)).any():
        sys.exit(f"портфель {T} уже в журнале — журнал не перезаписывается")
    ret, val, roll = load()
    idx = ret.index
    ends = month_ends(idx)
    periods = list(ends.index)
    if M not in periods:
        sys.exit(f"нет данных за {M}: сначала make momentum-update")
    k = periods.index(M)
    sig_day = ends[M]
    today = pd.Timestamp(date.today())
    if today.to_period("M") <= M and sig_day < today:
        # месяц ещё идёт, а сегодняшней расчётной цены в данных нет: сигнал вышел бы без последнего дня
        sys.exit(f"месяц {M} не закрыт: в данных последний день {sig_day.date()}, сегодня {today.date()}")
    if sig_day < pd.bdate_range(M.start_time, M.end_time)[-1] - pd.Timedelta(days=3):
        sys.exit(f"последний день данных за {M} — {sig_day.date()}: месяц не закончился или данные не догружены")
    R = ret.fillna(0).to_numpy()
    pos_of = {d: i for i, d in enumerate(idx)}
    w, ok, _ = target(ret, val, R, ends, periods, pos_of, k)
    status = "пробный" if T == TRIAL else ("засчитан" if T >= FIRST_COUNTED else "до регистрации")
    rows = []
    for a_i in np.where(w != 0)[0]:
        a = ret.columns[a_i]
        f = pd.read_csv(os.path.join(SRC, a + ".csv"), parse_dates=["date"]).set_index("date")
        settle, secid = f.loc[sig_day, "settle"], f.loc[sig_day, "secid"]
        rows.append({"month": str(T), "status": status, "signal_date": sig_day.date(), "asset": a,
                     "side": "лонг" if w[a_i] > 0 else "шорт", "weight": round(abs(w[a_i]), 6),
                     "secid": secid, "settle": settle, "contracts": int(round(CAPITAL * abs(w[a_i]) / settle)),
                     "universe": int(ok.sum()), "generated_at": datetime.now().isoformat(timespec="seconds")})
    if dry:
        print(f"репетиция: портфель {T} по сигналу на {sig_day.date()}, вселенная {int(ok.sum())}, "
              f"лонг {', '.join(r['asset'] for r in rows if r['side'] == 'лонг')}; "
              f"шорт {', '.join(r['asset'] for r in rows if r['side'] == 'шорт')}")
        return
    new = not os.path.exists(JOURNAL)
    with open(JOURNAL, "a", newline="") as fh:
        wr = csv.DictWriter(fh, fieldnames=FIELDS)
        if new:
            wr.writeheader()
        wr.writerows(rows)
    print(f"портфель {T} ({status}), сигнал на {sig_day.date()}, вселенная {int(ok.sum())}:")
    for r in rows:
        print(f"  {r['side']:4} {r['asset']:5} {r['secid']:10} вес {r['weight']:.3f}  контрактов {r['contracts']:4d} "
              f"(расч. цена {r['settle']})")
    print("Закоммить журнал ДО клиринга первого торгового дня месяца (~18:50 МСК).")


def score():
    j = read_journal()
    if j.empty:
        sys.exit("журнал пуст")
    ret, val, roll = load()
    daily, log = run(ret, val, roll)
    m = monthly(daily) * 1e4
    last_day = ret.index[-1]
    # Сверка: пересчёт тем же кодом обязан дать портфель журнала.
    log["month"] = log["date"].dt.to_period("M").astype(str)
    print(f"Данные по {last_day.date()}\n")
    print(f"{'месяц':8} {'статус':14} {'б.п.':>7}  сверка")
    counted = []
    for T, g in j.groupby("month", sort=True):
        L = sorted(g.loc[g.side == "лонг", "asset"])
        S = sorted(g.loc[g.side == "шорт", "asset"])
        lg = log[log["month"] == T]
        if lg.empty:
            print(f"{T:8} {g.status.iloc[0]:14} {'—':>7}  исполнения ещё нет в данных")
            continue
        same = sorted(lg["long"].iloc[0]) == L and sorted(lg["short"].iloc[0]) == S
        p = pd.Period(T)
        full = last_day >= p.end_time.normalize() - pd.Timedelta(days=3)
        v = m.get(p, np.nan)
        mark = "" if full else " (неполный)"
        print(f"{T:8} {g.status.iloc[0]:14} {v:+7.0f}  {'совпадает' if same else 'РАСХОЖДЕНИЕ: ' + str(lg['long'].iloc[0]) + ' / ' + str(lg['short'].iloc[0])}{mark}")
        if g.status.iloc[0] == "засчитан" and full:
            counted.append(v)
    n = len(counted)
    if n:
        c = np.array(counted)
        print(f"\nзасчитано месяцев {n}: среднее {c.mean():+.0f} б.п./мес, накоплено {c.sum() / 100:+.1f}%"
              + (f", t {nw_t(c, 3):+.2f}" if n >= 6 else ""))
        for g_n, thr in GATES.items():
            if n >= g_n:
                print(f"  точка {g_n} мес.: стоп, если среднее < {thr:+.0f} → {'СТОП' if c[:g_n].mean() < thr else 'продолжаем'}")
    else:
        print("\nзасчитанных полных месяцев пока нет")


if __name__ == "__main__":
    cmd = sys.argv[1]
    if cmd == "signal":
        signal(sys.argv[2] if len(sys.argv) > 2 else None)
    elif cmd == "signal-dry":
        signal(dry=True)
    else:
        score()
