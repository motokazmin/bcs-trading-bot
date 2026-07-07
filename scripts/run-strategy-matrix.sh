#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
OPT=bin/optimizer
COMMON=(
  -universe config/optimizer/universe.yaml
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
  local out="results/exp-${id}/"
  echo "========== $id $(date -Iseconds) =========="
  "$OPT" run -strategy "$id" -search-space "$space" "${COMMON[@]}" -output "$out"
}

run_strategy momentum_breakout config/optimizer/search-space-momentum.yaml
run_strategy momentum_filtered config/optimizer/search-space-momentum-filtered.yaml
run_strategy opening_range config/optimizer/search-space-orb.yaml
run_strategy mean_reversion config/optimizer/search-space-meanrev.yaml

python3 - <<'PY'
import json, glob

STRATEGIES = [
    "momentum_breakout",
    "momentum_filtered",
    "opening_range",
    "mean_reversion",
]

rows = []
for name in STRATEGIES:
    paths = sorted(glob.glob(f"results/exp-{name}/optimizer-run-*.json"))
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
    rows.append({
        "strategy": name,
        "run_file": path,
        "score": best.get("score"),
        "sum_pnl": total_pnl,
        "sum_trades": total_trades,
        "profitable_windows": prof,
        "num_windows": len(windows),
    })

rows.sort(key=lambda r: (r["score"] or -1e18), reverse=True)
out = {"experiments": rows}
with open("results/strategy-matrix-summary.json", "w") as f:
    json.dump(out, f, indent=2)

print("| strategy | score | sum_pnl | trades | prof_windows |")
print("|----------|-------|---------|--------|--------------|")
for r in rows:
    print(f"| {r['strategy']} | {r['score']} | {r['sum_pnl']:.0f} | {r['sum_trades']} | {r['profitable_windows']}/{r['num_windows']} |")
PY

echo "ALL DONE $(date -Iseconds)"
