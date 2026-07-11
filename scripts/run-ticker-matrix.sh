#!/usr/bin/env bash
# Per-ticker solo matrix: один тикер × N trials × одна стратегия.
set -euo pipefail
cd "$(dirname "$0")/.."

STRATEGY="${MATRIX_STRATEGY:-momentum_filtered}"
SEARCH_SPACE="${MATRIX_SEARCH_SPACE:-configs/strategies/momentum-filtered-afternoon.yaml}"
TRIALS="${MATRIX_TRIALS:-100}"
SEED="${MATRIX_SEED:-1}"
MIN_TRADES="${MATRIX_MIN_TRADES:-5}"
RESULTS_ROOT="${RESULTS_ROOT:-results/ticker-matrix/mf-afternoon}"
REGISTRY="${REGISTRY:-${RESULTS_ROOT}/runs-registry.json}"
OPT=bin/optimizer

TICKERS=(SBER GAZP LKOH NVTK ROSN MGNT CHMF TATN MOEX)

if [[ ! -x "$OPT" ]]; then
  make build-optimizer
fi

mkdir -p "$RESULTS_ROOT"

for ticker in "${TICKERS[@]}"; do
  RUN_ID="solo-${ticker}"
  OUTPUT_DIR="${RESULTS_ROOT}/${RUN_ID}"
  TICKERS_FILE="${RESULTS_ROOT}/.tickers-${ticker}.yaml"
  cat > "$TICKERS_FILE" <<EOF
class_code: TQBR
candle_timeframe: M5
initial_history_years: 2
costs:
  commission_per_lot: 0.10
tickers:
  - ${ticker}
EOF

  echo "========== ${STRATEGY} solo ${ticker} $(date -Iseconds) =========="
  "$OPT" run \
    -strategy "$STRATEGY" \
    -search-space "$SEARCH_SPACE" \
    -tickers-config "$TICKERS_FILE" \
    -history-dir data/history \
    -trials "$TRIALS" \
    -seed "$SEED" \
    -min-trades "$MIN_TRADES" \
    -stop-mode atr \
    -parallel 0 \
    -output "$OUTPUT_DIR/"

  python3 scripts/record-strategy-run.py \
    --strategy "$STRATEGY" \
    --run-id "$RUN_ID" \
    --output-dir "$OUTPUT_DIR" \
    --search-space "$SEARCH_SPACE" \
    --tickers "$TICKERS_FILE" \
    --trials "$TRIALS" \
    --seed "$SEED" \
    --registry "$REGISTRY" \
    --description "${STRATEGY} solo ${ticker}, ${TRIALS} trials"
done

python3 - <<'PY'
import json, os, sys
registry = os.environ.get("REGISTRY", "results/ticker-matrix/mf-afternoon/runs-registry.json")
with open(registry) as f:
    data = json.load(f)
rows = []
for r in data.get("runs", []):
    bt = r.get("backtest_best_config") or {}
    wf = r.get("walk_forward") or {}
    ticker = r["id"].replace("solo-", "")
    rows.append({
        "ticker": ticker,
        "exp_r": bt.get("expectancy_r"),
        "pf": bt.get("profit_factor"),
        "pnl": bt.get("total_pnl_rub"),
        "trades": bt.get("trade_count"),
        "wf": f"{wf.get('profitable_windows', '?')}/{wf.get('num_windows', '?')}",
    })
rows.sort(key=lambda x: x.get("exp_r") or -999, reverse=True)
print("\n=== Ticker matrix summary ===")
print(f"{'ticker':<6} {'exp_R':>7} {'PF':>5} {'pnl₽':>10} {'trades':>6} {'WF':>8}")
print("-" * 50)
for row in rows:
    exp = row["exp_r"]
    print(
        f"{row['ticker']:<6} "
        f"{(exp if exp is not None else float('nan')):>7.3f} "
        f"{(row['pf'] or 0):>5.2f} "
        f"{(row['pnl'] or 0):>10.0f} "
        f"{(row['trades'] or 0):>6} "
        f"{row['wf']:>8}"
    )
summary_path = os.path.join(os.path.dirname(registry), "summary.json")
with open(summary_path, "w") as f:
    json.dump({"rows": rows}, f, indent=2)
print(f"\nSummary: {summary_path}")
PY

echo "ALL DONE $(date -Iseconds)"
