#!/usr/bin/env bash
# Walk-forward оптимизация Prev-Day Level Breakout + запись в runs-registry.json
set -euo pipefail
cd "$(dirname "$0")/.."

RUN_ID="${PREV_DAY_RUN_ID:-wave1}"
SEARCH_SPACE="${PREV_DAY_SEARCH_SPACE:-configs/legacy/frequency-hypotheses/strategies/prev-day-level-breakout.yaml}"
TICKERS="${PREV_DAY_TICKERS:-configs/legacy/frequency-hypotheses/tickers/tickers-prev-day-mgnt-rosn-lkoh.yaml}"
TRIALS="${PREV_DAY_TRIALS:-200}"
SEED="${PREV_DAY_SEED:-1}"
MIN_TRADES="${PREV_DAY_MIN_TRADES:-10}"
RESULTS_ROOT="${RESULTS_ROOT:-results/prev-day-level}"
OUTPUT_DIR="${OUTPUT_DIR:-${RESULTS_ROOT}/${RUN_ID}}"
REGISTRY="${REGISTRY:-${RESULTS_ROOT}/runs-registry.json}"

mkdir -p "$RESULTS_ROOT"
OPT=bin/optimizer

if [[ ! -x "$OPT" ]]; then
  make build-optimizer
fi

echo "========== Prev-Day Level run_id=${RUN_ID} $(date -Iseconds) =========="
echo "  search_space: ${SEARCH_SPACE}"
echo "  tickers:      ${TICKERS}"
echo "  trials:       ${TRIALS}  seed: ${SEED}"
echo "  output:       ${OUTPUT_DIR}"

"$OPT" run \
  -strategy prev_day_level_breakout \
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
  --strategy prev_day_level_breakout \
  --run-id "$RUN_ID" \
  --output-dir "$OUTPUT_DIR" \
  --search-space "$SEARCH_SPACE" \
  --tickers "$TICKERS" \
  --trials "$TRIALS" \
  --seed "$SEED" \
  --registry "$REGISTRY" \
  --description "Prev-Day Level: ${SEARCH_SPACE}, ${TRIALS} trials, seed ${SEED}"

echo "DONE $(date -Iseconds)"
