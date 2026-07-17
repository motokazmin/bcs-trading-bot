#!/usr/bin/env bash
# Walk-forward оптимизация Midday Compression Breakout + запись в runs-registry.json
set -euo pipefail
cd "$(dirname "$0")/.."

RUN_ID="${MIDDAY_RUN_ID:-wave1}"
SEARCH_SPACE="${MIDDAY_SEARCH_SPACE:-configs/legacy/frequency-hypotheses/strategies/midday-compression-breakout.yaml}"
TICKERS="${MIDDAY_TICKERS:-configs/legacy/frequency-hypotheses/tickers/tickers-lkoh-moex.yaml}"
TRIALS="${MIDDAY_TRIALS:-300}"
SEED="${MIDDAY_SEED:-1}"
MIN_TRADES="${MIDDAY_MIN_TRADES:-10}"
RESULTS_ROOT="${RESULTS_ROOT:-results/midday-compression}"
OUTPUT_DIR="${OUTPUT_DIR:-${RESULTS_ROOT}/${RUN_ID}}"
REGISTRY="${REGISTRY:-${RESULTS_ROOT}/runs-registry.json}"

mkdir -p "$RESULTS_ROOT"
OPT=bin/optimizer

if [[ ! -x "$OPT" ]]; then
  make build-optimizer
fi

echo "========== Midday Compression run_id=${RUN_ID} $(date -Iseconds) =========="
echo "  search_space: ${SEARCH_SPACE}"
echo "  tickers:      ${TICKERS}"
echo "  trials:       ${TRIALS}  seed: ${SEED}"
echo "  output:       ${OUTPUT_DIR}"

"$OPT" run \
  -strategy midday_compression_breakout \
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
  --strategy midday_compression_breakout \
  --run-id "$RUN_ID" \
  --output-dir "$OUTPUT_DIR" \
  --search-space "$SEARCH_SPACE" \
  --tickers "$TICKERS" \
  --trials "$TRIALS" \
  --seed "$SEED" \
  --registry "$REGISTRY" \
  --description "Midday Compression: ${SEARCH_SPACE}, ${TRIALS} trials, seed ${SEED}"

echo "DONE $(date -Iseconds)"
