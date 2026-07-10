# BCS Trading Bot — удобные команды запуска
#
# Требуется: export BCS_REFRESH_TOKEN=...
#
# Сборка:     make build
# Тесты:      make test
# История:    make sync-history          (первый раз ~2 года, далее — догрузка)
# Optimizer:  make optimizer-run
# Бот:        make bot

BINARY_DIR := bin
GO := go

# Основной путь к shared-списку тикеров для optimizer.
TICKERS_CONFIG    ?= configs/shared/tickers-orc.yaml
# Legacy-алиас для совместимости со старыми локальными override.
UNIVERSE          ?= $(TICKERS_CONFIG)
HISTORY_DIR       ?= data/history
SEARCH_SPACE      ?= configs/strategies/orc.yaml
OPTIMIZER_STRATEGY ?= opening_range_continuation
OPTIMIZER_OUT     ?= results/orc/
PARALLEL_TICKERS  ?= 5
OPTIMIZER_PARALLEL ?= 0
OPTIMIZER_TWO_PHASE ?=

BOT_CONFIG ?= configs/runs/experiments-all.yaml

.PHONY: build build-bot build-optimizer build-admin test \
        sync-history optimizer-run optimizer-orc optimizer-momentum optimizer-or-fade optimizer-focus strategy-matrix charts-all \
        bot bot-futures bot-real bot-smoke admin help

help:
	@echo "BCS Trading Bot — make targets"
	@echo ""
	@echo "  make build              — собрать bot, optimizer, admin"
	@echo "  make test               — go test ./..."
	@echo ""
	@echo "  make sync-history       — догрузить историю (9 акций, параллельно)"
	@echo "  make optimizer-run      — sync-history + walk-forward ORC"
	@echo "  make optimizer-orc        — ORC wave2 (300 trials) → results/orc/"
	@echo "  make optimizer-or-fade      — OR Fade wave1 (300 trials) → results/or-fade/"
	@echo "  make optimizer-focus      — alias для optimizer-orc"
	@echo "  make charts-all         — HTML-графики по всем экспериментам в results/"
	@echo ""
	@echo "  make bot                — paper, все A/B-эксперименты (experiments-all.yaml)"
	@echo "  make bot-futures        — paper, фьючерсы SPBFUT"
	@echo "  make bot-real           — реальная торговля"
	@echo "  make bot-smoke          — smoke test OAuth+WS"
	@echo "  make admin              — веб-админка"
	@echo ""
	@echo "Переменные: TICKERS_CONFIG, HISTORY_DIR, PARALLEL_TICKERS, OPTIMIZER_PARALLEL,"
	@echo "            OPTIMIZER_TWO_PHASE=1, OPTIMIZER_STRATEGY, SEARCH_SPACE, OPTIMIZER_OUT,"
	@echo "            BOT_CONFIG, BCS_REFRESH_TOKEN"

build: build-bot build-optimizer build-admin

build-bot:
	@mkdir -p $(BINARY_DIR)
	$(GO) build -o $(BINARY_DIR)/bot ./cmd/bot

build-optimizer:
	@mkdir -p $(BINARY_DIR)
	$(GO) build -o $(BINARY_DIR)/optimizer ./cmd/optimizer

build-admin:
	@mkdir -p $(BINARY_DIR)
	$(GO) build -o $(BINARY_DIR)/admin ./cmd/admin

test:
	$(GO) test ./...

# --- История для optimizer ---

sync-history: build-optimizer
	$(BINARY_DIR)/optimizer sync-history \
		-tickers-config $(TICKERS_CONFIG) \
		-parallel-tickers $(PARALLEL_TICKERS) \
		-output-dir $(HISTORY_DIR)

# --- Optimizer ---

optimizer-run: build-optimizer sync-history
	$(BINARY_DIR)/optimizer run \
		-strategy $(OPTIMIZER_STRATEGY) \
		-tickers-config $(TICKERS_CONFIG) \
		-history-dir $(HISTORY_DIR) \
		-search-space $(SEARCH_SPACE) \
		-parallel $(OPTIMIZER_PARALLEL) \
		$(if $(OPTIMIZER_TWO_PHASE),-two-phase,) \
		-output $(OPTIMIZER_OUT)

strategy-matrix: build-optimizer
	chmod +x scripts/run-strategy-matrix.sh
	mkdir -p results
	bash scripts/run-strategy-matrix.sh 2>&1 | tee results/strategy-matrix-run.log

optimizer-orc: build-optimizer
	chmod +x scripts/run-orc-optimizer.sh
	mkdir -p results/orc
	bash scripts/run-orc-optimizer.sh 2>&1 | tee results/orc/last-run.log

optimizer-focus: optimizer-orc

optimizer-momentum: build-optimizer
	chmod +x scripts/run-momentum-optimizer.sh
	mkdir -p results/momentum
	bash scripts/run-momentum-optimizer.sh 2>&1 | tee results/momentum/last-run.log

optimizer-or-fade: build-optimizer
	chmod +x scripts/run-or-fade-optimizer.sh
	mkdir -p results/or-fade
	bash scripts/run-or-fade-optimizer.sh 2>&1 | tee results/or-fade/last-run.log

charts-all: build-optimizer
	$(BINARY_DIR)/optimizer charts -all \
		-results-dir $(OPTIMIZER_OUT) \
		-history-dir $(HISTORY_DIR)

# --- Бот ---

bot: build-bot
	$(BINARY_DIR)/bot -config $(BOT_CONFIG)

bot-futures: build-bot
	$(BINARY_DIR)/bot -config configs/runs/virtual-futures.yaml

bot-real: build-bot
	$(BINARY_DIR)/bot -config configs/runs/real-stocks.yaml

bot-smoke: build-bot
	$(BINARY_DIR)/bot -config $(BOT_CONFIG) -smoke-test

# --- Админка ---

admin: build-admin
	$(BINARY_DIR)/admin
