#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
RESULTS_DIR="${RESULTS_DIR:-results}"
mkdir -p "$RESULTS_DIR"
OPT=bin/optimizer
COMMON=(
  -tickers-config configs/shared/tickers.yaml
  -history-dir data/history
  -trials 200
  -seed 1
  -min-trades 10
  -stop-mode atr
  -parallel 0
)

run_strategy() {
  local id="$1"
  local space="$2"
  local out="${RESULTS_DIR}/exp-${id}/"
  echo "========== $id $(date -Iseconds) =========="
  "$OPT" run -strategy "$id" -search-space "$space" "${COMMON[@]}" -output "$out"
}

run_strategy momentum_breakout configs/strategies/momentum-breakout.yaml
run_strategy momentum_filtered configs/strategies/momentum-filtered.yaml
run_strategy opening_range configs/strategies/opening-range.yaml
run_strategy opening_range_continuation configs/strategies/orc.yaml
run_strategy mean_reversion configs/strategies/mean-reversion.yaml

RESULTS_DIR="$RESULTS_DIR" python3 - <<'PY'
import json, glob, os

results_dir = os.environ.get("RESULTS_DIR", "results")

def side_stats(trades):
    stats = {}
    for t in trades:
        side = (t.get("Direction") or "").upper()
        if side not in ("BUY", "SELL"):
            continue
        item = stats.setdefault(side, {"trades": 0, "wins": 0, "gross_pnl": 0.0})
        item["trades"] += 1
        if t.get("IsWinner"):
            item["wins"] += 1
        item["gross_pnl"] += float(t.get("GrossPnL") or 0.0)
    for item in stats.values():
        trades_count = item["trades"]
        item["win_rate"] = (item["wins"] / trades_count * 100.0) if trades_count else 0.0
    return stats

rows = []
for exp_dir in sorted(glob.glob(f"{results_dir}/exp-*/")):
    name = os.path.basename(exp_dir.rstrip("/")).removeprefix("exp-")
    paths = sorted(glob.glob(f"{exp_dir}/optimizer-run-*.json"))
    if not paths:
        continue
    path = paths[-1]
    with open(path) as f:
        d = json.load(f)
    best = d.get("best") or (d["trials"][0] if d.get("trials") else {})
    windows = best.get("windows") or []
    total_pnl = sum(w["metrics"]["total_pnl"] for w in windows)
    total_trades = sum(w["metrics"]["num_trades"] for w in windows)
    prof = sum(1 for w in windows if w["metrics"]["total_pnl"] > 0)
    trades_path = f"{exp_dir}/export/data-trades.json"
    strategy_side_stats = {}
    ticker_side_stats = {}
    if os.path.exists(trades_path):
        with open(trades_path) as f:
            trades_export = json.load(f)
        experiments = trades_export.get("experiments") or []
        if experiments:
            trades = experiments[0].get("trades") or []
            strategy_side_stats = side_stats(trades)
            ticker_trades = {}
            for t in trades:
                ticker = t.get("Ticker") or ""
                if not ticker:
                    continue
                ticker_trades.setdefault(ticker, []).append(t)
            ticker_side_stats = {
                ticker: side_stats(ticker_rows)
                for ticker, ticker_rows in ticker_trades.items()
            }
    rows.append({
        "strategy": name,
        "run_file": path,
        "score": best.get("score"),
        "sum_pnl": total_pnl,
        "sum_trades": total_trades,
        "profitable_windows": prof,
        "num_windows": len(windows),
        "by_side": strategy_side_stats,
        "by_ticker_side": ticker_side_stats,
    })

rows.sort(key=lambda r: (r["score"] or -1e18), reverse=True)
summary_path = f"{results_dir}/strategy-matrix-summary.json"
with open(summary_path, "w") as f:
    json.dump({"experiments": rows}, f, indent=2)

print(f"Summary: {summary_path} ({len(rows)} strategies)")
print("| strategy | score | sum_pnl | trades | prof_windows |")
print("|----------|-------|---------|--------|--------------|")
for r in rows:
    print(f"| {r['strategy']} | {r['score']} | {r['sum_pnl']:.0f} | {r['sum_trades']} | {r['profitable_windows']}/{r['num_windows']} |")
    side = r.get("by_side") or {}
    buy = side.get("BUY", {})
    sell = side.get("SELL", {})
    print(
        f"  buy: {buy.get('wins', 0)}/{buy.get('trades', 0)} "
        f"({buy.get('win_rate', 0.0):.1f}%) | "
        f"sell: {sell.get('wins', 0)}/{sell.get('trades', 0)} "
        f"({sell.get('win_rate', 0.0):.1f}%)"
    )
PY

echo "ALL DONE $(date -Iseconds)"
