#!/usr/bin/env bash
# Walk-forward оптимизация Afternoon Range Fade + запись в runs-registry.json
set -euo pipefail
cd "$(dirname "$0")/.."

RUN_ID="${AFTERNOON_FADE_RUN_ID:-wave1}"
SEARCH_SPACE="${AFTERNOON_FADE_SEARCH_SPACE:-configs/legacy/frequency-hypotheses/strategies/afternoon-range-fade.yaml}"
TICKERS="${AFTERNOON_FADE_TICKERS:-configs/shared/tickers-or-fade-conservative.yaml}"
TRIALS="${AFTERNOON_FADE_TRIALS:-200}"
SEED="${AFTERNOON_FADE_SEED:-1}"
MIN_TRADES="${AFTERNOON_FADE_MIN_TRADES:-10}"
RESULTS_ROOT="${RESULTS_ROOT:-results/afternoon-range-fade}"
OUTPUT_DIR="${OUTPUT_DIR:-${RESULTS_ROOT}/${RUN_ID}}"
REGISTRY="${REGISTRY:-${RESULTS_ROOT}/runs-registry.json}"

mkdir -p "$RESULTS_ROOT"
OPT=bin/optimizer

if [[ ! -x "$OPT" ]]; then
  make build-optimizer
fi

echo "========== Afternoon Range Fade run_id=${RUN_ID} $(date -Iseconds) =========="
echo "  search_space: ${SEARCH_SPACE}"
echo "  tickers:      ${TICKERS}"
echo "  trials:       ${TRIALS}  seed: ${SEED}"
echo "  output:       ${OUTPUT_DIR}"

"$OPT" run \
  -strategy afternoon_range_fade \
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
  --strategy afternoon_range_fade \
  --run-id "$RUN_ID" \
  --output-dir "$OUTPUT_DIR" \
  --search-space "$SEARCH_SPACE" \
  --tickers "$TICKERS" \
  --trials "$TRIALS" \
  --seed "$SEED" \
  --registry "$REGISTRY" \
  --description "Afternoon Range Fade: ${SEARCH_SPACE}, ${TRIALS} trials, seed ${SEED}"

echo "DONE $(date -Iseconds)"
