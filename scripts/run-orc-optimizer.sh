#!/usr/bin/env bash
# Walk-forward оптимизация ORC + запись в runs-registry.json
#
# Примеры:
#   ./scripts/run-orc-optimizer.sh
#   ORC_RUN_ID=wave2 ORC_TRIALS=300 ./scripts/run-orc-optimizer.sh
#   ORC_SEARCH_SPACE=configs/strategies/orc-wave1-wide.yaml ORC_RUN_ID=wave1-rerun ./scripts/run-orc-optimizer.sh
set -euo pipefail
cd "$(dirname "$0")/.."

ORC_RUN_ID="${ORC_RUN_ID:-wave2}"
ORC_SEARCH_SPACE="${ORC_SEARCH_SPACE:-configs/strategies/orc.yaml}"
ORC_TICKERS="${ORC_TICKERS:-configs/shared/tickers-orc.yaml}"
ORC_TRIALS="${ORC_TRIALS:-300}"
ORC_SEED="${ORC_SEED:-1}"
ORC_MIN_TRADES="${ORC_MIN_TRADES:-10}"
RESULTS_ROOT="${RESULTS_ROOT:-results/orc}"
OUTPUT_DIR="${OUTPUT_DIR:-${RESULTS_ROOT}/${ORC_RUN_ID}}"

mkdir -p "$RESULTS_ROOT"
OPT=bin/optimizer

if [[ ! -x "$OPT" ]]; then
  make build-optimizer
fi

echo "========== ORC run_id=${ORC_RUN_ID} $(date -Iseconds) =========="
echo "  search_space: ${ORC_SEARCH_SPACE}"
echo "  tickers:      ${ORC_TICKERS}"
echo "  trials:       ${ORC_TRIALS}  seed: ${ORC_SEED}"
echo "  output:       ${OUTPUT_DIR}"

"$OPT" run \
  -strategy opening_range_continuation \
  -search-space "$ORC_SEARCH_SPACE" \
  -tickers-config "$ORC_TICKERS" \
  -history-dir data/history \
  -trials "$ORC_TRIALS" \
  -seed "$ORC_SEED" \
  -min-trades "$ORC_MIN_TRADES" \
  -stop-mode atr \
  -parallel 0 \
  -output "$OUTPUT_DIR/"

python3 scripts/record-orc-run.py \
  --run-id "$ORC_RUN_ID" \
  --output-dir "$OUTPUT_DIR" \
  --search-space "$ORC_SEARCH_SPACE" \
  --tickers "$ORC_TICKERS" \
  --trials "$ORC_TRIALS" \
  --seed "$ORC_SEED" \
  --description "ORC optimizer: ${ORC_SEARCH_SPACE}, ${ORC_TRIALS} trials, seed ${ORC_SEED}"

echo "DONE $(date -Iseconds)"
