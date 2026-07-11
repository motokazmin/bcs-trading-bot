#!/usr/bin/env bash
# Walk-forward оптимизация afternoon-стратегий + запись в runs-registry.json
set -euo pipefail
cd "$(dirname "$0")/.."

AFT_STRATEGY="${AFT_STRATEGY:-mean_reversion}"
AFT_RUN_ID="${AFT_RUN_ID:-wave1}"
AFT_SEARCH_SPACE="${AFT_SEARCH_SPACE:-configs/strategies/mean-reversion-afternoon.yaml}"
AFT_TICKERS="${AFT_TICKERS:-configs/shared/tickers-orc-no-sber.yaml}"
AFT_TRIALS="${AFT_TRIALS:-200}"
AFT_SEED="${AFT_SEED:-1}"
AFT_MIN_TRADES="${AFT_MIN_TRADES:-10}"
RESULTS_ROOT="${RESULTS_ROOT:-results/afternoon}"
OUTPUT_DIR="${OUTPUT_DIR:-${RESULTS_ROOT}/${AFT_RUN_ID}}"
REGISTRY="${REGISTRY:-${RESULTS_ROOT}/runs-registry.json}"

mkdir -p "$RESULTS_ROOT"
OPT=bin/optimizer

if [[ ! -x "$OPT" ]]; then
  make build-optimizer
fi

echo "========== Afternoon ${AFT_STRATEGY} run_id=${AFT_RUN_ID} $(date -Iseconds) =========="
echo "  search_space: ${AFT_SEARCH_SPACE}"
echo "  tickers:      ${AFT_TICKERS}"
echo "  trials:       ${AFT_TRIALS}  seed: ${AFT_SEED}"
echo "  output:       ${OUTPUT_DIR}"

"$OPT" run \
  -strategy "$AFT_STRATEGY" \
  -search-space "$AFT_SEARCH_SPACE" \
  -tickers-config "$AFT_TICKERS" \
  -history-dir data/history \
  -trials "$AFT_TRIALS" \
  -seed "$AFT_SEED" \
  -min-trades "$AFT_MIN_TRADES" \
  -stop-mode atr \
  -parallel 0 \
  -output "$OUTPUT_DIR/"

python3 scripts/record-strategy-run.py \
  --strategy "$AFT_STRATEGY" \
  --run-id "$AFT_RUN_ID" \
  --output-dir "$OUTPUT_DIR" \
  --search-space "$AFT_SEARCH_SPACE" \
  --tickers "$AFT_TICKERS" \
  --trials "$AFT_TRIALS" \
  --seed "$AFT_SEED" \
  --registry "$REGISTRY" \
  --description "Afternoon ${AFT_STRATEGY}: ${AFT_SEARCH_SPACE}, ${AFT_TRIALS} trials, seed ${AFT_SEED}"

echo "DONE $(date -Iseconds)"
