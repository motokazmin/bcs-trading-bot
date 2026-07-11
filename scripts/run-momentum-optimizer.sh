#!/usr/bin/env bash
# Walk-forward оптимизация momentum_breakout + запись в runs-registry.json
#
# Примеры:
#   ./scripts/run-momentum-optimizer.sh
#   MOM_RUN_ID=wave1-lean MOM_TICKERS=configs/shared/tickers-momentum-lean.yaml ./scripts/run-momentum-optimizer.sh
set -euo pipefail
cd "$(dirname "$0")/.."

MOM_RUN_ID="${MOM_RUN_ID:-wave1-wide}"
MOM_SEARCH_SPACE="${MOM_SEARCH_SPACE:-configs/strategies/momentum-day.yaml}"
MOM_TICKERS="${MOM_TICKERS:-configs/shared/tickers-momentum.yaml}"
MOM_TRIALS="${MOM_TRIALS:-200}"
MOM_SEED="${MOM_SEED:-1}"
MOM_MIN_TRADES="${MOM_MIN_TRADES:-20}"
RESULTS_ROOT="${RESULTS_ROOT:-results/momentum}"
OUTPUT_DIR="${OUTPUT_DIR:-${RESULTS_ROOT}/${MOM_RUN_ID}}"
REGISTRY="${REGISTRY:-${RESULTS_ROOT}/runs-registry.json}"

mkdir -p "$RESULTS_ROOT"
OPT=bin/optimizer

if [[ ! -x "$OPT" ]]; then
  make build-optimizer
fi

echo "========== momentum_breakout run_id=${MOM_RUN_ID} $(date -Iseconds) =========="
echo "  search_space: ${MOM_SEARCH_SPACE}"
echo "  tickers:      ${MOM_TICKERS}"
echo "  trials:       ${MOM_TRIALS}  seed: ${MOM_SEED}"
echo "  min_trades:   ${MOM_MIN_TRADES}"
echo "  output:       ${OUTPUT_DIR}"

"$OPT" run \
  -strategy momentum_breakout \
  -search-space "$MOM_SEARCH_SPACE" \
  -tickers-config "$MOM_TICKERS" \
  -history-dir data/history \
  -trials "$MOM_TRIALS" \
  -seed "$MOM_SEED" \
  -min-trades "$MOM_MIN_TRADES" \
  -stop-mode atr \
  -parallel 0 \
  -output "$OUTPUT_DIR/"

python3 scripts/record-strategy-run.py \
  --strategy momentum_breakout \
  --run-id "$MOM_RUN_ID" \
  --output-dir "$OUTPUT_DIR" \
  --search-space "$MOM_SEARCH_SPACE" \
  --tickers "$MOM_TICKERS" \
  --trials "$MOM_TRIALS" \
  --seed "$MOM_SEED" \
  --registry "$REGISTRY" \
  --description "momentum_breakout: ${MOM_SEARCH_SPACE}, ${MOM_TRIALS} trials, seed ${MOM_SEED}"

echo "DONE $(date -Iseconds)"
