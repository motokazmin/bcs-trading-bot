#!/usr/bin/env bash
# Устаревший alias: только ORC (см. scripts/run-orc-optimizer.sh).
set -euo pipefail
cd "$(dirname "$0")/.."
export ORC_RUN_ID="${ORC_RUN_ID:-wave2}"
export RESULTS_ROOT="${RESULTS_ROOT:-results/orc}"
exec bash scripts/run-orc-optimizer.sh
