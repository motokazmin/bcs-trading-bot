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
TICKERS_CONFIG    ?= configs/shared/tickers.yaml
# Legacy-алиас для совместимости со старыми локальными override.
UNIVERSE          ?= $(TICKERS_CONFIG)
HISTORY_DIR       ?= data/history
SEARCH_SPACE      ?= configs/strategies/orc.yaml
OPTIMIZER_OUT     ?= results/
PARALLEL_TICKERS  ?= 5
OPTIMIZER_PARALLEL ?= 0
OPTIMIZER_TWO_PHASE ?=

BOT_CONFIG ?= configs/runs/experiments-all.yaml

.PHONY: build build-bot build-optimizer build-admin test \
        sync-history optimizer-run strategy-matrix \
        bot bot-futures bot-real bot-smoke admin help

help:
	@echo "BCS Trading Bot — make targets"
	@echo ""
	@echo "  make build              — собрать bot, optimizer, admin"
	@echo "  make test               — go test ./..."
	@echo ""
	@echo "  make sync-history       — догрузить историю (9 акций, параллельно)"
	@echo "  make optimizer-run      — sync-history + walk-forward оптимизация"
	@echo ""
	@echo "  make bot                — paper, все A/B-эксперименты (experiments-all.yaml)"
	@echo "  make bot-futures        — paper, фьючерсы SPBFUT"
	@echo "  make bot-real           — реальная торговля"
	@echo "  make bot-smoke          — smoke test OAuth+WS"
	@echo "  make admin              — веб-админка"
	@echo ""
	@echo "Переменные: TICKERS_CONFIG, HISTORY_DIR, PARALLEL_TICKERS, OPTIMIZER_PARALLEL,"
	@echo "            OPTIMIZER_TWO_PHASE=1, SEARCH_SPACE, OPTIMIZER_OUT, BOT_CONFIG, BCS_REFRESH_TOKEN"

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
		-tickers-config $(TICKERS_CONFIG) \
		-history-dir $(HISTORY_DIR) \
		-search-space $(SEARCH_SPACE) \
		-parallel $(OPTIMIZER_PARALLEL) \
		$(if $(OPTIMIZER_TWO_PHASE),-two-phase,) \
		-output $(OPTIMIZER_OUT)

strategy-matrix: build-optimizer
	chmod +x scripts/run-strategy-matrix.sh
	bash scripts/run-strategy-matrix.sh 2>&1 | tee results/strategy-matrix-run.log

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
