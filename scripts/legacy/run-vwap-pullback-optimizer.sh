#!/usr/bin/env bash
# Walk-forward оптимизация VWAP Pullback Continuation + запись в runs-registry.json
set -euo pipefail
cd "$(dirname "$0")/.."

RUN_ID="${VWAP_RUN_ID:-wave1}"
SEARCH_SPACE="${VWAP_SEARCH_SPACE:-configs/legacy/frequency-hypotheses/strategies/vwap-pullback-continuation.yaml}"
TICKERS="${VWAP_TICKERS:-configs/shared/tickers-orc-no-sber.yaml}"
TRIALS="${VWAP_TRIALS:-300}"
SEED="${VWAP_SEED:-1}"
MIN_TRADES="${VWAP_MIN_TRADES:-10}"
RESULTS_ROOT="${RESULTS_ROOT:-results/vwap-pullback}"
OUTPUT_DIR="${OUTPUT_DIR:-${RESULTS_ROOT}/${RUN_ID}}"
REGISTRY="${REGISTRY:-${RESULTS_ROOT}/runs-registry.json}"

mkdir -p "$RESULTS_ROOT"
OPT=bin/optimizer

if [[ ! -x "$OPT" ]]; then
  make build-optimizer
fi

echo "========== VWAP Pullback run_id=${RUN_ID} $(date -Iseconds) =========="
echo "  search_space: ${SEARCH_SPACE}"
echo "  tickers:      ${TICKERS}"
echo "  trials:       ${TRIALS}  seed: ${SEED}"
echo "  output:       ${OUTPUT_DIR}"

"$OPT" run \
  -strategy vwap_pullback_continuation \
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
  --strategy vwap_pullback_continuation \
  --run-id "$RUN_ID" \
  --output-dir "$OUTPUT_DIR" \
  --search-space "$SEARCH_SPACE" \
  --tickers "$TICKERS" \
  --trials "$TRIALS" \
  --seed "$SEED" \
  --registry "$REGISTRY" \
  --description "VWAP Pullback: ${SEARCH_SPACE}, ${TRIALS} trials, seed ${SEED}"

echo "DONE $(date -Iseconds)"
