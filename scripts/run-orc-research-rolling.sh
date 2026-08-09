#!/usr/bin/env bash
# Rolling research ORC: полный universe, allow_all_tickers, недавнее окно.
#
# Примеры:
#   ./scripts/run-orc-research-rolling.sh
#   ORC_DATE_FROM=2025-01-01 ORC_DATE_TO=2026-05-08 ORC_TRIALS=300 ./scripts/run-orc-research-rolling.sh
#
# После прогона: скопируйте strategy из results/research/orc-rolling/*/best-config-*.yaml
# в configs/research/orc-main-wide.yaml и configs/runs/portfolio-research-orc.yaml
# (tickers оставить полными, allow_all_tickers: true).
set -euo pipefail
cd "$(dirname "$0")/.."

ORC_SEARCH_SPACE="${ORC_SEARCH_SPACE:-configs/strategies/orc-research-rolling.yaml}"
ORC_TICKERS="${ORC_TICKERS:-configs/shared/tickers.yaml}"
ORC_TRIALS="${ORC_TRIALS:-200}"
ORC_SEED="${ORC_SEED:-1}"
ORC_MIN_TRADES="${ORC_MIN_TRADES:-15}"
ORC_WINDOW_MONTHS="${ORC_WINDOW_MONTHS:-3}"
ORC_STEP_MONTHS="${ORC_STEP_MONTHS:-1}"
# In-sample ближе к сейчас; holdout отдельно (см. docs/research-rolling.md)
ORC_DATE_FROM="${ORC_DATE_FROM:-2025-05-08}"
ORC_DATE_TO="${ORC_DATE_TO:-2026-05-08}"
RESULTS_ROOT="${RESULTS_ROOT:-results/research/orc-rolling}"
RUN_ID="${ORC_RUN_ID:-$(date +%Y%m%d)}"
OUTPUT_DIR="${OUTPUT_DIR:-${RESULTS_ROOT}/${RUN_ID}}"

mkdir -p "$OUTPUT_DIR"
OPT=bin/optimizer

if [[ ! -x "$OPT" ]]; then
  make build-optimizer
fi

echo "========== ORC research rolling $(date -Iseconds) =========="
echo "  search_space: ${ORC_SEARCH_SPACE}"
echo "  tickers:      ${ORC_TICKERS}"
echo "  period:       ${ORC_DATE_FROM} → ${ORC_DATE_TO}"
echo "  windows:      ${ORC_WINDOW_MONTHS}m / step ${ORC_STEP_MONTHS}m"
echo "  trials:       ${ORC_TRIALS}  seed: ${ORC_SEED}"
echo "  output:       ${OUTPUT_DIR}"

"$OPT" run \
  -strategy opening_range_continuation \
  -search-space "$ORC_SEARCH_SPACE" \
  -tickers-config "$ORC_TICKERS" \
  -history-dir data/history \
  -date-from "$ORC_DATE_FROM" \
  -date-to "$ORC_DATE_TO" \
  -window-months "$ORC_WINDOW_MONTHS" \
  -step-months "$ORC_STEP_MONTHS" \
  -trials "$ORC_TRIALS" \
  -seed "$ORC_SEED" \
  -min-trades "$ORC_MIN_TRADES" \
  -stop-mode atr \
  -parallel 0 \
  -output "$OUTPUT_DIR/"

echo "DONE $(date -Iseconds)"
echo "Next: copy strategy from ${OUTPUT_DIR}/best-config-*.yaml → configs/research/orc-main-wide.yaml"
echo "Then: go run ./cmd/optimizer portfolio-backtest -config configs/runs/portfolio-research-orc.yaml \\"
echo "        -date-from ${ORC_DATE_TO} -date-to 2026-08-08   # holdout example"
