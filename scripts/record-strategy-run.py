#!/usr/bin/env python3
"""Записывает результат optimizer-прогона в runs-registry.json."""
from __future__ import annotations

import argparse
import json
import os
from datetime import datetime, timezone
from glob import glob
from pathlib import Path


def load_json(path: str) -> dict | None:
    try:
        with open(path, encoding="utf-8") as f:
            return json.load(f)
    except OSError:
        return None


def latest_glob(directory: str, pattern: str) -> str | None:
    matches = sorted(glob(os.path.join(directory, pattern)))
    return matches[-1] if matches else None


def collect_run(
    run_id: str,
    output_dir: str,
    strategy: str,
    search_space: str,
    tickers_config: str,
    trials: int,
    seed: int,
    description: str,
) -> dict:
    opt_path = latest_glob(output_dir, "optimizer-run-*.json")
    best_cfg = latest_glob(output_dir, "best-config-*.yaml")
    summary_path = os.path.join(output_dir, "export", "data-summary.json")

    walk_forward = {}
    best_params: dict = {}
    if opt_path:
        opt = load_json(opt_path) or {}
        best = opt.get("best") or {}
        windows = best.get("windows") or []
        best_params = best.get("params") or {}
        walk_forward = {
            "score": best.get("score"),
            "sum_pnl_windows": sum(w.get("metrics", {}).get("total_pnl", 0) for w in windows),
            "sum_trades_windows": sum(w.get("metrics", {}).get("num_trades", 0) for w in windows),
            "profitable_windows": sum(1 for w in windows if w.get("metrics", {}).get("total_pnl", 0) > 0),
            "num_windows": len(windows),
        }

    backtest: dict = {}
    by_ticker: list = []
    if summary_path:
        summary = load_json(summary_path) or {}
        km = summary.get("key_metrics") or {}
        backtest = {
            "expectancy_r": km.get("expectancy_r"),
            "expectancy_rub": km.get("expectancy_rub"),
            "total_pnl_rub": km.get("total_pnl_rub"),
            "win_rate": km.get("win_rate"),
            "profit_factor": km.get("profit_factor"),
            "trade_count": km.get("trade_count"),
            "date_from": (summary.get("date_range") or {}).get("from"),
            "date_to": (summary.get("date_range") or {}).get("to"),
        }
        exps = summary.get("experiments") or []
        if exps:
            by_ticker = [
                {
                    "ticker": row.get("key"),
                    "trades": row.get("trade_count"),
                    "avg_pnl_r": row.get("avg_pnl_r"),
                    "total_pnl": row.get("total_pnl"),
                    "profit_factor": row.get("profit_factor"),
                }
                for row in exps[0].get("by_ticker") or []
            ]

    return {
        "id": run_id,
        "recorded_at": datetime.now(timezone.utc).isoformat(),
        "description": description,
        "config": {
            "search_space": search_space,
            "tickers": tickers_config,
            "trials": trials,
            "seed": seed,
        },
        "artifacts": {
            "output_dir": output_dir,
            "optimizer_run": opt_path,
            "best_config": best_cfg,
            "export_summary": summary_path if os.path.isfile(summary_path) else None,
            "export_trades": os.path.join(output_dir, "export", "data-trades.json"),
        },
        "walk_forward": walk_forward,
        "backtest_best_config": backtest,
        "best_params": best_params,
        "by_ticker": by_ticker,
    }


def upsert_registry(registry_path: Path, strategy: str, entry: dict) -> list[dict]:
    data = {"strategy": strategy, "runs": []}
    if registry_path.is_file():
        loaded = load_json(str(registry_path))
        if loaded:
            data = loaded
    data["strategy"] = strategy
    runs = [r for r in data.get("runs", []) if r.get("id") != entry["id"]]
    runs.append(entry)
    runs.sort(key=lambda r: r.get("recorded_at", ""))
    data["runs"] = runs
    registry_path.parent.mkdir(parents=True, exist_ok=True)
    with open(registry_path, "w", encoding="utf-8") as f:
        json.dump(data, f, indent=2, ensure_ascii=False)
        f.write("\n")
    return runs


def print_comparison(strategy: str, runs: list[dict]) -> None:
    print(f"\n=== {strategy} runs comparison ===")
    header = (
        f"{'id':<20} {'exp_R':>7} {'PF':>5} {'wr%':>5} "
        f"{'pnl₽':>10} {'trades':>6} {'wf_win':>8} {'score':>7}"
    )
    print(header)
    print("-" * len(header))
    for r in runs:
        bt = r.get("backtest_best_config") or {}
        wf = r.get("walk_forward") or {}
        exp_r = bt.get("expectancy_r")
        pf = bt.get("profit_factor")
        wr = bt.get("win_rate")
        pnl = bt.get("total_pnl_rub")
        trades = bt.get("trade_count")
        wf_win = f"{wf.get('profitable_windows', '?')}/{wf.get('num_windows', '?')}"
        score = wf.get("score")
        print(
            f"{r.get('id', '?'):<20} "
            f"{(exp_r if exp_r is not None else float('nan')):>7.3f} "
            f"{(pf if pf is not None else 0):>5.2f} "
            f"{(wr if wr is not None else 0):>5.1f} "
            f"{(pnl if pnl is not None else 0):>10.0f} "
            f"{(trades if trades is not None else 0):>6} "
            f"{wf_win:>8} "
            f"{(score if score is not None else 0):>7.3f}"
        )


def main() -> None:
    p = argparse.ArgumentParser(description="Record optimizer run to strategy registry")
    p.add_argument("--strategy", required=True, help="strategy id, e.g. momentum_breakout")
    p.add_argument("--run-id", required=True)
    p.add_argument("--output-dir", required=True)
    p.add_argument("--search-space", required=True)
    p.add_argument("--tickers", required=True)
    p.add_argument("--trials", type=int, default=200)
    p.add_argument("--seed", type=int, default=1)
    p.add_argument("--description", default="")
    p.add_argument("--registry", required=True, help="path to runs-registry.json")
    args = p.parse_args()

    entry = collect_run(
        run_id=args.run_id,
        output_dir=args.output_dir,
        strategy=args.strategy,
        search_space=args.search_space,
        tickers_config=args.tickers,
        trials=args.trials,
        seed=args.seed,
        description=args.description or f"{args.strategy} run {args.run_id}",
    )
    runs = upsert_registry(Path(args.registry), args.strategy, entry)
    print(f"Recorded: {args.registry} ← {args.run_id}")
    print_comparison(args.strategy, runs)


if __name__ == "__main__":
    main()
