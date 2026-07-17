#!/usr/bin/env bash
# Walk-forward для extended sessions (утро / вечер / ДСВД).
#
# Примеры:
#   SESSION_SLOT=evening SESSION_STRAT=orc ./scripts/run-session-optimizer.sh
#   SESSION_SLOT=morning SESSION_STRAT=gap-drive SESSION_TRIALS=200 ./scripts/run-session-optimizer.sh
set -euo pipefail
cd "$(dirname "$0")/.."

SESSION_SLOT="${SESSION_SLOT:-evening}"   # evening|morning|weekend
SESSION_STRAT="${SESSION_STRAT:-orc}"     # orc|or-fade|gap-drive
SESSION_RUN_ID="${SESSION_RUN_ID:-wave1-${SESSION_SLOT}-${SESSION_STRAT}}"
SESSION_TRIALS="${SESSION_TRIALS:-200}"
SESSION_SEED="${SESSION_SEED:-1}"
SESSION_MIN_TRADES="${SESSION_MIN_TRADES:-10}"
TICKERS="${SESSION_TICKERS:-configs/shared/tickers-session-liquid.yaml}"

case "$SESSION_STRAT" in
  orc) STRATEGY_ID=session_orc ;;
  or-fade) STRATEGY_ID=session_or_fade ;;
  gap-drive) STRATEGY_ID=session_gap_drive ;;
  *) echo "unknown SESSION_STRAT=$SESSION_STRAT"; exit 1 ;;
esac

SEARCH_SPACE="configs/strategies/session-${SESSION_STRAT}-${SESSION_SLOT}.yaml"
RESULTS_ROOT="results/session-${SESSION_SLOT}-${SESSION_STRAT}"
OUTPUT_DIR="${OUTPUT_DIR:-${RESULTS_ROOT}/${SESSION_RUN_ID}}"

mkdir -p "$RESULTS_ROOT"
OPT=bin/optimizer
if [[ ! -x "$OPT" ]]; then
  make build-optimizer
fi

echo "========== session ${SESSION_SLOT}/${SESSION_STRAT} run_id=${SESSION_RUN_ID} $(date -Iseconds) =========="
echo "  strategy:     ${STRATEGY_ID}"
echo "  search_space: ${SEARCH_SPACE}"
echo "  tickers:      ${TICKERS}"
echo "  trials:       ${SESSION_TRIALS}  seed: ${SESSION_SEED}"
echo "  output:       ${OUTPUT_DIR}"

"$OPT" run \
  -strategy "$STRATEGY_ID" \
  -search-space "$SEARCH_SPACE" \
  -tickers-config "$TICKERS" \
  -history-dir data/history \
  -trials "$SESSION_TRIALS" \
  -seed "$SESSION_SEED" \
  -min-trades "$SESSION_MIN_TRADES" \
  -stop-mode atr \
  -parallel 0 \
  -output "$OUTPUT_DIR/"

python3 scripts/record-strategy-run.py \
  --run-id "$SESSION_RUN_ID" \
  --output-dir "$OUTPUT_DIR" \
  --strategy "$STRATEGY_ID" \
  --search-space "$SEARCH_SPACE" \
  --tickers "$TICKERS" \
  --trials "$SESSION_TRIALS" \
  --seed "$SESSION_SEED" \
  --registry "${RESULTS_ROOT}/runs-registry.json" \
  --description "session ${SESSION_SLOT} ${SESSION_STRAT}: ${SEARCH_SPACE}, ${SESSION_TRIALS} trials, seed ${SESSION_SEED}"

echo "DONE $(date -Iseconds)"
