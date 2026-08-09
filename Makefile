# BCS Trading Bot — удобные команды запуска
#
# Требуется: export BCS_REFRESH_TOKEN=...
# Облако:    export ADMIN_TOKEN=... HTTP_LISTEN=0.0.0.0:8091
# Логи:      по умолчанию /var/log/trading-bot/bot.log; только stdout: LOG_FILE=-
#
# Сборка:     make build
# Тесты:      make test
# История:    make sync-history          (первый раз ~2 года, далее — догрузка)
# Optimizer:  make optimizer-run
# Бот:        make bot / make bot-stop / make bot-status

BINARY_DIR := bin
GO := go

# Основной путь к shared-списку тикеров для optimizer run (strategy-specific).
TICKERS_CONFIG    ?= configs/shared/tickers-orc.yaml
# Sync всегда тянет полный universe, не whitelist стратегии.
SYNC_TICKERS_CONFIG ?= configs/shared/tickers.yaml
# Алиас: UNIVERSE = TICKERS_CONFIG (локальные override).
UNIVERSE          ?= $(TICKERS_CONFIG)
HISTORY_DIR       ?= data/history
SEARCH_SPACE      ?= configs/strategies/orc.yaml
OPTIMIZER_STRATEGY ?= opening_range_continuation
OPTIMIZER_OUT     ?= results/orc/
PARALLEL_TICKERS  ?= 5
OPTIMIZER_PARALLEL ?= 0
OPTIMIZER_TWO_PHASE ?=

BOT_CONFIG ?= configs/runs/portfolio-paper.yaml
# Админка: локально 127.0.0.1:8091; в облаке HTTP_LISTEN=0.0.0.0:8091 и ADMIN_TOKEN=...
HTTP_LISTEN ?= 127.0.0.1:8091
# Дублировать логи в файл (дефолт /var/log/trading-bot/bot.log). Только stdout: LOG_FILE=- make bot
LOG_FILE ?= /var/log/trading-bot/bot.log
LOG_FILE_FLAG := -log-file $(LOG_FILE)
BOT_PID_FILE ?= data/bot.pid

.PHONY: build build-bot build-optimizer test \
        sync-history optimizer-run optimizer-orc optimizer-orc-research optimizer-momentum optimizer-or-fade optimizer-afternoon optimizer-focus strategy-matrix charts-all \
        bot bot-futures bot-real bot-smoke bot-stop bot-status help

help:
	@echo "BCS Trading Bot — make targets"
	@echo "Шпаргалка: docs/runbook.md  (локально vs облако, админка, optimizer)"
	@echo ""
	@echo "  make build              — собрать bot, optimizer"
	@echo "  make test               — go test ./..."
	@echo ""
	@echo "  make sync-history       — догрузить полный universe (tickers.yaml), параллельно"
	@echo "  make optimizer-run      — sync-history + walk-forward ORC"
	@echo "  make optimizer-orc      — ORC → results/orc/  (champions — только по запросу)"
	@echo "  make optimizer-orc-research — ORC rolling wide → results/research/orc-rolling/"
	@echo "  make optimizer-or-fade  — OR Fade → results/or-fade/"
	@echo "  make optimizer-afternoon — MF afternoon → results/afternoon/"
	@echo "  make optimizer-focus    — alias для optimizer-orc"
	@echo "  make charts-all         — HTML-графики по OPTIMIZER_OUT"
	@echo ""
	@echo "  make bot                — paper portfolio в фоне; админка HTTP_LISTEN (дефолт 127.0.0.1:8091)"
	@echo "  make bot-futures        — paper фьючерсы в фоне (не portfolio)"
	@echo "  make bot-real           — реальная торговля в фоне"
	@echo "  make bot-stop           — остановить фоновый bot (PID в BOT_PID_FILE)"
	@echo "  make bot-status         — статус фонового bot"
	@echo "  make bot-smoke          — smoke test OAuth+WS (foreground)"
	@echo ""
	@echo "Облако:  export ADMIN_TOKEN=... HTTP_LISTEN=0.0.0.0:8091 && make bot"
	@echo "Локально: make bot → http://127.0.0.1:8091  |  make bot-stop"
	@echo "Логи:     /var/log/trading-bot/bot.log по умолчанию; LOG_FILE=- (только stdout → data/bot.stdout.log)"
	@echo ""
	@echo "Переменные: TICKERS_CONFIG (run), SYNC_TICKERS_CONFIG (sync), HISTORY_DIR, PARALLEL_TICKERS, OPTIMIZER_PARALLEL,"
	@echo "            OPTIMIZER_TWO_PHASE=1, OPTIMIZER_STRATEGY, SEARCH_SPACE, OPTIMIZER_OUT,"
	@echo "            BOT_CONFIG, HTTP_LISTEN, LOG_FILE, BOT_PID_FILE, BCS_REFRESH_TOKEN, ADMIN_TOKEN"

build: build-bot build-optimizer

build-bot:
	@mkdir -p $(BINARY_DIR)
	$(GO) build -o $(BINARY_DIR)/bot ./cmd/bot

build-optimizer:
	@mkdir -p $(BINARY_DIR)
	$(GO) build -o $(BINARY_DIR)/optimizer ./cmd/optimizer

test:
	$(GO) test ./...

# --- История для optimizer ---

sync-history: build-optimizer
	$(BINARY_DIR)/optimizer sync-history \
		-tickers-config $(SYNC_TICKERS_CONFIG) \
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

optimizer-orc-research: build-optimizer
	chmod +x scripts/run-orc-research-rolling.sh
	mkdir -p results/research/orc-rolling
	bash scripts/run-orc-research-rolling.sh 2>&1 | tee results/research/orc-rolling/last-run.log

optimizer-focus: optimizer-orc

optimizer-momentum: build-optimizer
	chmod +x scripts/run-momentum-optimizer.sh
	mkdir -p results/momentum
	bash scripts/run-momentum-optimizer.sh 2>&1 | tee results/momentum/last-run.log

optimizer-or-fade: build-optimizer
	chmod +x scripts/run-or-fade-optimizer.sh
	mkdir -p results/or-fade
	bash scripts/run-or-fade-optimizer.sh 2>&1 | tee results/or-fade/last-run.log

optimizer-afternoon: build-optimizer
	chmod +x scripts/run-afternoon-optimizer.sh
	mkdir -p results/afternoon
	bash scripts/run-afternoon-optimizer.sh 2>&1 | tee results/afternoon/last-run.log


charts-all: build-optimizer
	$(BINARY_DIR)/optimizer charts -all \
		-results-dir $(OPTIMIZER_OUT) \
		-history-dir $(HISTORY_DIR)

# --- Бот (фон: make bot / bot-stop / bot-status; smoke — foreground) ---

# Если LOG_FILE пустой или "-", stdout/stderr → data/bot.stdout.log; иначе — /dev/null (логи пишет сам бот).
define bot_stdout_redirect
$(if $(filter -,$(LOG_FILE)),data/bot.stdout.log,$(if $(LOG_FILE),/dev/null,data/bot.stdout.log))
endef

define start_bot_bg
	@if [ -f "$(BOT_PID_FILE)" ] && kill -0 $$(cat "$(BOT_PID_FILE)") 2>/dev/null; then \
		echo "bot уже запущен pid=$$(cat $(BOT_PID_FILE)) (make bot-stop)"; \
		exit 1; \
	fi
	@mkdir -p "$$(dirname "$(BOT_PID_FILE)")"
	@nohup $(BINARY_DIR)/bot -config $(1) -http-listen $(HTTP_LISTEN) $(LOG_FILE_FLAG) \
		> "$(bot_stdout_redirect)" 2>&1 & echo $$! > "$(BOT_PID_FILE)"
	@sleep 0.3
	@if ! kill -0 $$(cat "$(BOT_PID_FILE)") 2>/dev/null; then \
		echo "bot не стартовал — смотри логи ($(LOG_FILE) / $(bot_stdout_redirect))"; \
		rm -f "$(BOT_PID_FILE)"; \
		exit 1; \
	fi
	@echo "bot started pid=$$(cat $(BOT_PID_FILE)) listen=$(HTTP_LISTEN) log=$(LOG_FILE) pidfile=$(BOT_PID_FILE)"
endef

bot: build-bot
	$(call start_bot_bg,$(BOT_CONFIG))

bot-futures: build-bot
	$(call start_bot_bg,configs/runs/virtual-futures.yaml)

bot-real: build-bot
	$(call start_bot_bg,configs/runs/real-stocks.yaml)

bot-stop:
	@if [ ! -f "$(BOT_PID_FILE)" ]; then \
		echo "bot не запущен (нет $(BOT_PID_FILE))"; \
	else \
		pid=$$(cat "$(BOT_PID_FILE)"); \
		if kill -0 $$pid 2>/dev/null; then \
			kill $$pid; \
			for i in 1 2 3 4 5 6 7 8 9 10; do \
				kill -0 $$pid 2>/dev/null || break; \
				sleep 0.3; \
			done; \
			if kill -0 $$pid 2>/dev/null; then \
				echo "bot pid=$$pid не завершился, kill -9"; \
				kill -9 $$pid 2>/dev/null || true; \
			fi; \
			echo "bot stopped pid=$$pid"; \
		else \
			echo "stale pidfile $(BOT_PID_FILE) (процесс $$pid нет)"; \
		fi; \
		rm -f "$(BOT_PID_FILE)"; \
	fi

bot-status:
	@if [ ! -f "$(BOT_PID_FILE)" ]; then \
		echo "bot: stopped"; \
	else \
		pid=$$(cat "$(BOT_PID_FILE)"); \
		if kill -0 $$pid 2>/dev/null; then \
			echo "bot: running pid=$$pid listen=$(HTTP_LISTEN) log=$(LOG_FILE)"; \
		else \
			echo "bot: stopped (stale pidfile $(BOT_PID_FILE))"; \
		fi; \
	fi

bot-smoke: build-bot
	$(BINARY_DIR)/bot -config $(BOT_CONFIG) -smoke-test $(LOG_FILE_FLAG)
