#!/usr/bin/env bash
# Walk-forward оптимизация Momentum SBER day-trend + запись в runs-registry.json
set -euo pipefail
cd "$(dirname "$0")/.."

RUN_ID="${SBER_DT_RUN_ID:-wave1}"
SEARCH_SPACE="${SBER_DT_SEARCH_SPACE:-configs/legacy/frequency-hypotheses/strategies/momentum-sber-daytrend.yaml}"
TICKERS="${SBER_DT_TICKERS:-configs/shared/tickers-mf-afternoon-sber.yaml}"
TRIALS="${SBER_DT_TRIALS:-300}"
SEED="${SBER_DT_SEED:-1}"
MIN_TRADES="${SBER_DT_MIN_TRADES:-10}"
RESULTS_ROOT="${RESULTS_ROOT:-results/momentum-sber-daytrend}"
OUTPUT_DIR="${OUTPUT_DIR:-${RESULTS_ROOT}/${RUN_ID}}"
REGISTRY="${REGISTRY:-${RESULTS_ROOT}/runs-registry.json}"

mkdir -p "$RESULTS_ROOT"
OPT=bin/optimizer

if [[ ! -x "$OPT" ]]; then
  make build-optimizer
fi

echo "========== Momentum SBER daytrend run_id=${RUN_ID} $(date -Iseconds) =========="
echo "  search_space: ${SEARCH_SPACE}"
echo "  tickers:      ${TICKERS}"
echo "  trials:       ${TRIALS}  seed: ${SEED}"
echo "  output:       ${OUTPUT_DIR}"

"$OPT" run \
  -strategy momentum_sber_daytrend \
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
  --strategy momentum_sber_daytrend \
  --run-id "$RUN_ID" \
  --output-dir "$OUTPUT_DIR" \
  --search-space "$SEARCH_SPACE" \
  --tickers "$TICKERS" \
  --trials "$TRIALS" \
  --seed "$SEED" \
  --registry "$REGISTRY" \
  --description "Momentum SBER daytrend: ${SEARCH_SPACE}, ${TRIALS} trials, seed ${SEED}"

echo "DONE $(date -Iseconds)"
