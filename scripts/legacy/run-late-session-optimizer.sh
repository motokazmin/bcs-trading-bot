#!/usr/bin/env bash
# Walk-forward оптимизация Late Session Imbalance + запись в runs-registry.json
set -euo pipefail
cd "$(dirname "$0")/.."

RUN_ID="${LATE_RUN_ID:-wave1}"
SEARCH_SPACE="${LATE_SEARCH_SPACE:-configs/legacy/frequency-hypotheses/strategies/late-session-imbalance.yaml}"
TICKERS="${LATE_TICKERS:-configs/legacy/frequency-hypotheses/tickers/tickers-lkoh-moex.yaml}"
TRIALS="${LATE_TRIALS:-300}"
SEED="${LATE_SEED:-1}"
MIN_TRADES="${LATE_MIN_TRADES:-10}"
RESULTS_ROOT="${RESULTS_ROOT:-results/late-session}"
OUTPUT_DIR="${OUTPUT_DIR:-${RESULTS_ROOT}/${RUN_ID}}"
REGISTRY="${REGISTRY:-${RESULTS_ROOT}/runs-registry.json}"

mkdir -p "$RESULTS_ROOT"
OPT=bin/optimizer

if [[ ! -x "$OPT" ]]; then
  make build-optimizer
fi

echo "========== Late Session Imbalance run_id=${RUN_ID} $(date -Iseconds) =========="
echo "  search_space: ${SEARCH_SPACE}"
echo "  tickers:      ${TICKERS}"
echo "  trials:       ${TRIALS}  seed: ${SEED}"
echo "  output:       ${OUTPUT_DIR}"

"$OPT" run \
  -strategy late_session_imbalance \
  -search-space "$SEARCH_SPACE" \
  -tickers-config "$TICKERS" \
  -history-dir data/history \
  -trials "$TRIALS" \
  -seed "$SEED" \
  -min-trades "$MIN_TRADES" \
  -stop-mode atr \
  -parallel 0 \
  -output "$OUTPUT_DIR/"

python3 scripts/record-strategy-run.py \
  --strategy late_session_imbalance \
  --run-id "$RUN_ID" \
  --output-dir "$OUTPUT_DIR" \
  --search-space "$SEARCH_SPACE" \
  --tickers "$TICKERS" \
  --trials "$TRIALS" \
  --seed "$SEED" \
  --registry "$REGISTRY" \
  --description "Late Session Imbalance: ${SEARCH_SPACE}, ${TRIALS} trials, seed ${SEED}"

echo "DONE $(date -Iseconds)"
