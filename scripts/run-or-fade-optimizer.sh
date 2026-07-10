#!/usr/bin/env bash
# Walk-forward оптимизация OR Fade + запись в runs-registry.json
set -euo pipefail
cd "$(dirname "$0")/.."

ORF_RUN_ID="${ORF_RUN_ID:-wave1}"
ORF_SEARCH_SPACE="${ORF_SEARCH_SPACE:-configs/strategies/or-fade.yaml}"
ORF_TICKERS="${ORF_TICKERS:-configs/shared/tickers-orc-no-sber.yaml}"
ORF_TRIALS="${ORF_TRIALS:-300}"
ORF_SEED="${ORF_SEED:-1}"
ORF_MIN_TRADES="${ORF_MIN_TRADES:-10}"
RESULTS_ROOT="${RESULTS_ROOT:-results/or-fade}"
OUTPUT_DIR="${OUTPUT_DIR:-${RESULTS_ROOT}/${ORF_RUN_ID}}"
REGISTRY="${REGISTRY:-${RESULTS_ROOT}/runs-registry.json}"

mkdir -p "$RESULTS_ROOT"
OPT=bin/optimizer

if [[ ! -x "$OPT" ]]; then
  make build-optimizer
fi

echo "========== OR Fade run_id=${ORF_RUN_ID} $(date -Iseconds) =========="
echo "  search_space: ${ORF_SEARCH_SPACE}"
echo "  tickers:      ${ORF_TICKERS}"
echo "  trials:       ${ORF_TRIALS}  seed: ${ORF_SEED}"
echo "  output:       ${OUTPUT_DIR}"

"$OPT" run \
  -strategy opening_range_fade \
  -search-space "$ORF_SEARCH_SPACE" \
  -tickers-config "$ORF_TICKERS" \
  -history-dir data/history \
  -trials "$ORF_TRIALS" \
  -seed "$ORF_SEED" \
  -min-trades "$ORF_MIN_TRADES" \
  -stop-mode atr \
  -parallel 0 \
  -output "$OUTPUT_DIR/"

python3 scripts/record-strategy-run.py \
  --strategy opening_range_fade \
  --run-id "$ORF_RUN_ID" \
  --output-dir "$OUTPUT_DIR" \
  --search-space "$ORF_SEARCH_SPACE" \
  --tickers "$ORF_TICKERS" \
  --trials "$ORF_TRIALS" \
  --seed "$ORF_SEED" \
  --registry "$REGISTRY" \
  --description "OR Fade: ${ORF_SEARCH_SPACE}, ${ORF_TRIALS} trials, seed ${ORF_SEED}"

echo "DONE $(date -Iseconds)"
