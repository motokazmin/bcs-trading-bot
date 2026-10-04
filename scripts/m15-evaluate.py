#!/usr/bin/env python3
"""Оценка кандидатов 0014: лучший набор каждой стратегии → однослотовый конфиг →
portfolio-backtest на подборе и на хвосте. Критерий — в docs/analysis/0014-m15-champion-search.md.

Сверка переноса параметров: тот же набор гоняется движком оптимизатора (`optimizer backtest`
с замороженным пространством). Потерянный при конвертации в YAML параметр меняет число сделок.

Запуск: python3 scripts/m15-evaluate.py
"""
import glob
import json
import os
import re
import subprocess
import sys

import yaml

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
RES = os.environ.get("M15_RESULTS") or os.path.join(ROOT, "results", "m15")
OPT = os.path.join(ROOT, "bin", "optimizer")
TICKERS = ["AFKS", "CHMF", "GAZP", "LKOH", "MGNT", "MOEX", "NVTK", "ROSN", "SBER", "TATN"]
FIT = ("2024-10-04", "2026-03-31")
HOLD = ("2026-04-01", "2026-10-02")
SLOTS = [
    ("session-orc-morning", "session_orc"),
    ("orc-day", "session_orc"),
    ("or-fade", "session_or_fade"),
    ("mf-afternoon", "momentum_filtered"),
    ("session-orc-evening", "session_orc"),
]
# Параметры, которые уходят не в strategy, или пишутся под другим именем.
RISK_KEYS = {"riskPerTradePercent", "dailyLossLimitPercent", "maxParallelTrades"}
RENAMED = {
    "maxEntriesPerTickerPerDay": "max_trades_per_ticker_per_day",
    "volumeFilterMultiplier": "volume_min_ratio",
}


def snake(name):
    return RENAMED.get(name) or re.sub(r"(?<!^)([A-Z])", r"_\1", name).lower().replace("s_m_a", "sma")


def load_best(name):
    files = sorted(glob.glob(os.path.join(RES, name, "optimizer-run-*.json")))
    if not files:
        sys.exit(f"{name}: нет результата подбора в {RES}/{name}")
    best = json.load(open(files[-1]))["best"]
    cfgs = sorted(glob.glob(os.path.join(RES, name, "best-config-*.yaml")))
    return best, yaml.safe_load(open(cfgs[-1]))


def build_config(name, best, best_cfg):
    space = yaml.safe_load(open(os.path.join(ROOT, "configs", "strategies", "m15", name + ".yaml")))
    strat = dict(best_cfg["strategy"])
    lost = []
    for k, v in best["params"].items():
        if k in RISK_KEYS:
            continue
        key = snake(k)
        if key not in strat:
            lost.append(f"{k}→{key}")
            strat[key] = v
    sess = space["session"]
    exp = {
        "id": name,
        "session_open_time": sess["session_open_time"],
        "eod_close_time": sess["eod_close_time"],
        "entry_delay_minutes": sess.get("entry_delay_minutes", 0),
        "weekdays_only": bool(sess.get("weekdays_only", False)),
        "tickers": TICKERS,
        "strategy": strat,
    }
    cfg = {
        "trading_mode": "virtual",
        "tickers": TICKERS,
        "class_code": "TQBR",
        "candle_timeframe": "M15",
        "costs": {"commission_rate_per_leg": 0.00008, "slippage_bps": 1},
        "risk": {"deposit": 200000, "max_daily_loss_percent": 2.0,
                 "risk_per_trade_percent": 0.20, "max_parallel_trades": 5},
        "virtual": {"balance": 200000},
        "session": {"timezone": "Europe/Moscow", "session_open_time": "10:00", "eod_close_time": "18:40"},
        "storage": {"path": os.path.join(RES, name, "slot.db")},
        "experiments": [exp],
    }
    path = os.path.join(RES, name, "slot.yaml")
    with open(path, "w") as f:
        f.write(f"# 0014: замороженный лучший набор {name}, не править.\n")
        yaml.safe_dump(cfg, f, allow_unicode=True, sort_keys=False)
    return path, lost


def frozen_space(name, strategy_id, best):
    """Пространство, где всё зафиксировано лучшим набором: движок оптимизатора без YAML-конвертации."""
    src = yaml.safe_load(open(os.path.join(ROOT, "configs", "strategies", "m15", name + ".yaml")))
    params = dict(best["params"])
    k0 = "rewardRatio"
    out = {
        "candle_timeframe": "M15",
        "session": src["session"],
        "search_space": {
            "strategy": strategy_id,
            # LoadSearchSpace требует хотя бы один параметр: вырожденный диапазон.
            "parameters": {k0: {"type": "float", "min": params[k0], "max": params[k0]}},
            "fixed": {k: v for k, v in params.items() if k != k0},
        },
    }
    path = os.path.join(RES, name, "frozen-space.yaml")
    with open(path, "w") as f:
        yaml.safe_dump(out, f, allow_unicode=True, sort_keys=False)
    return path


def portfolio(cfg, period, slip):
    out = subprocess.run(
        [OPT, "portfolio-backtest", "-config", cfg, "-timeframe", "M15",
         "-date-from", period[0], "-date-to", period[1], "-slippage-bps", str(slip)],
        capture_output=True, text=True, cwd=ROOT)
    m = re.search(r"trades=(\d+) net_pnl=(\S+) exp_R=(\S+) exp_rub=\S+ PF=(\S+) win_rate=(\S+)%", out.stdout)
    if not m:
        sys.exit(f"portfolio-backtest {cfg}:\n{out.stdout}\n{out.stderr}")
    return {"trades": int(m[1]), "pnl": float(m[2]), "exp_r": float(m[3]), "pf": float(m[4]), "wr": float(m[5])}


def engine_trades(strategy_id, space, period):
    out = subprocess.run(
        [OPT, "backtest", "-strategy", strategy_id, "-search-space", space,
         "-tickers-config", "configs/shared/tickers.yaml", "-tickers", ",".join(TICKERS),
         "-timeframe", "M15", "-date-from", period[0], "-date-to", period[1]],
        capture_output=True, text=True, cwd=ROOT)
    m = re.search(r"trades=(\d+)", out.stdout)
    if not m:
        sys.exit(f"backtest {space}:\n{out.stdout}\n{out.stderr}")
    return int(m[1])


def main():
    rows = []
    for name, sid in SLOTS:
        best, best_cfg = load_best(name)
        cfg, lost = build_config(name, best, best_cfg)
        fit = portfolio(cfg, FIT, 1)
        h1 = portfolio(cfg, HOLD, 1)
        h2 = portfolio(cfg, HOLD, 2)
        eng = engine_trades(sid, frozen_space(name, sid, best), FIT)
        c1 = h1["trades"] >= 30
        c2 = h2["exp_r"] > 0
        c3 = fit["exp_r"] > 0 and h1["exp_r"] >= 0.5 * fit["exp_r"]
        rows.append((name, best["score"], fit, h1, h2, eng, lost, c1, c2, c3))

    print(f"подбор {FIT[0]}…{FIT[1]}, хвост {HOLD[0]}…{HOLD[1]}, M15, 10 тикеров\n")
    hdr = f"{'стратегия':22} {'score':>6} | {'подбор 1бп':>22} | {'хвост 1бп':>22} | {'хвост 2бп':>14} | ≥30 >0@2 ≥½ | итог"
    print(hdr)
    print("-" * len(hdr))
    for name, score, fit, h1, h2, eng, lost, c1, c2, c3 in rows:
        ok = c1 and c2 and c3
        yn = lambda b: " да" if b else "нет"
        print(f"{name:22} {score:6.2f} | {fit['trades']:5d} {fit['exp_r']:+.3f} PF {fit['pf']:4.2f} | "
              f"{h1['trades']:5d} {h1['exp_r']:+.3f} PF {h1['pf']:4.2f} | {h2['exp_r']:+.3f} PF {h2['pf']:4.2f} | "
              f"{yn(c1)} {yn(c2)} {yn(c3)} | {'КАНДИДАТ' if ok else 'отклонена'}")
    print("\nсверка переноса (сделок на подборе: портфель по YAML / движок оптимизатора по параметрам):")
    for name, score, fit, h1, h2, eng, lost, *_ in rows:
        note = f"  дописаны в YAML: {', '.join(lost)}" if lost else ""
        print(f"  {name:22} {fit['trades']:5d} / {eng:5d}{note}")


if __name__ == "__main__":
    main()
