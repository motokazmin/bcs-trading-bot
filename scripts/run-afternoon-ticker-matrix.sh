#!/usr/bin/env bash
# Per-ticker solo matrix: mean_reversion afternoon (100 trials each).
set -euo pipefail
cd "$(dirname "$0")/.."

TRIALS="${MATRIX_TRIALS:-100}"
SEED="${MATRIX_SEED:-1}"
SEARCH_SPACE="${MATRIX_SEARCH_SPACE:-configs/strategies/mean-reversion-afternoon.yaml}"
RESULTS_ROOT="${RESULTS_ROOT:-results/afternoon/ticker-matrix}"
REGISTRY="${REGISTRY:-results/afternoon/ticker-matrix/runs-registry.json}"
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

  echo "========== Matrix solo ${ticker} $(date -Iseconds) =========="
  "$OPT" run \
    -strategy mean_reversion \
    -search-space "$SEARCH_SPACE" \
    -tickers-config "$TICKERS_FILE" \
    -history-dir data/history \
    -trials "$TRIALS" \
    -seed "$SEED" \
    -min-trades 5 \
    -stop-mode atr \
    -parallel 0 \
    -output "$OUTPUT_DIR/"

  python3 scripts/record-strategy-run.py \
    --strategy mean_reversion \
    --run-id "$RUN_ID" \
    --output-dir "$OUTPUT_DIR" \
    --search-space "$SEARCH_SPACE" \
    --tickers "$TICKERS_FILE" \
    --trials "$TRIALS" \
    --seed "$SEED" \
    --registry "$REGISTRY" \
    --description "MR afternoon solo ${ticker}, ${TRIALS} trials"
done

echo "ALL DONE $(date -Iseconds)"
