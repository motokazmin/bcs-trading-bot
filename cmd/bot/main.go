package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"bcs-trading-bot/internal/api"
	"bcs-trading-bot/internal/bcs"
	"bcs-trading-bot/internal/config"
	"bcs-trading-bot/internal/datafeed"
	"bcs-trading-bot/internal/engine"
	"bcs-trading-bot/internal/live"
	"bcs-trading-bot/internal/risk"
	"bcs-trading-bot/internal/storage/sqlite"
	"bcs-trading-bot/internal/strategies/adapter"
	"bcs-trading-bot/pkg/interfaces"
	"bcs-trading-bot/internal/logx"
	"bcs-trading-bot/internal/models"
)

func main() {
	defaultLogFile := "/var/log/trading-bot/bot.log"
	if v := strings.TrimSpace(os.Getenv("LOG_FILE")); v != "" {
		defaultLogFile = v
	}

	configPath := flag.String("config", "configs/runs/portfolio-paper.yaml", "путь к YAML-конфигу")
	noColor := flag.Bool("no-color", false, "отключить цветной вывод в терминале")
	logFile := flag.String("log-file", defaultLogFile, "лог в файл + stdout (дефолт /var/log/trading-bot/bot.log; пустая строка или \"-\" — только stdout)")
	smokeTest := flag.Bool("smoke-test", false, "быстрая проверка: OAuth + WebSocket + виртуальная сделка без записи в БД")
	httpListen := flag.String("http-listen", "127.0.0.1:8091", "адрес HTTP UI/API админки (пустая строка — отключить)")
	archivesPath := flag.String("archives", "data/archives.json", "путь к JSON с архивами периодов")
	flag.Parse()

	logPath := strings.TrimSpace(*logFile)
	if logPath == "-" {
		logPath = ""
	}
	if logPath != "" {
		closer, err := logx.OpenFile(logPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "log-file %s: %v\n", logPath, err)
			os.Exit(1)
		}
		defer closer.Close()
	} else if *noColor {
		logx.SetColorEnabled(false)
	}

	logx.Info("Запуск торгового робота БКС на Go...")
	if logPath != "" {
		logx.Info("Лог-файл: %s (stdout + файл)", logPath)
	}

	token := os.Getenv("BCS_REFRESH_TOKEN")
	if token == "" {
		logx.Fatal("задайте переменную окружения BCS_REFRESH_TOKEN")
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		logx.Fatalf("ошибка загрузки конфига: %v", err)
	}

	experiments := cfg.ResolvedExperiments()

	client := bcs.NewBCSClient(token)
	client.SetClassCode(cfg.ClassCode)
	client.SetCandleTimeFrame(cfg.CandleTimeFrame)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	expNames := make([]string, len(experiments))
	for i, exp := range experiments {
		if exp.Name != "" && exp.Name != exp.ID {
			expNames[i] = fmt.Sprintf("%s (%s)", exp.ID, exp.Name)
		} else {
			expNames[i] = exp.ID
		}
	}

	logx.Info(
		"Конфиг: %s | Режим: %s | Тикеры: %s | Эксперименты: %s | Класс: %s | Свечи: %s",
		*configPath,
		cfg.TradingMode,
		strings.Join(cfg.AllTickerSymbols(), ", "),
		strings.Join(expNames, ", "),
		cfg.ClassCode,
		cfg.CandleTimeFrame,
	)

	logx.Info("Шаг 1: Авторизация через БКС OAuth...")
	if err := client.Connect(ctx); err != nil {
		logx.Fatalf("авторизация провалена: %v", err)
	}
	logx.Info("Access Token получен.")

	if *smokeTest {
		runSmokeTest(ctx, cfg, client)
		return
	}

	var tradeStore interfaces.TradeStore = interfaces.NoopTradeStore{}
	var tradeReader interfaces.TradeReader
	if cfg.StorageEnabled() {
		store, err := sqlite.Open(cfg.Storage.Path)
		if err != nil {
			logx.Fatalf("ошибка открытия БД сделок: %v", err)
		}
		tradeStore = store
		tradeReader = store
		defer func() {
			if err := store.Close(); err != nil {
				logx.Warn("ошибка закрытия БД: %v", err)
			}
		}()
		logx.Info("Хранилище сделок: %s", cfg.Storage.Path)
	}

	runID := fmt.Sprintf("%s-%s", filepath.Base(*configPath), time.Now().Format("20060102-150405"))

	accountRisk := cfg.AccountRisk()
	maxParallel := accountRisk.MaxParallelTrades
	if maxParallel <= 0 {
		maxParallel = 5
	}
	riskPct := accountRisk.RiskPerTradePercent
	if riskPct <= 0 {
		riskPct = 0.5
	}
	pct := accountRisk.MaxDailyLossPercent
	if pct <= 0 {
		pct = 2.0
	}
	globalRisk := risk.NewGlobalRiskController(accountRisk.Deposit, pct, riskPct, maxParallel)
	logx.Info(
		"Единый счёт: депозит %.0f | CB %.1f%% | open_risk_budget=%.0f ₽ (%.1f%%×%d слотов) | one-position-per-ticker",
		accountRisk.Deposit,
		pct,
		globalRisk.MaxOpenRiskBudgetLimit(),
		riskPct,
		maxParallel,
	)

	var executor interfaces.OrderExecutor
	switch cfg.TradingMode {
	case config.TradingModeVirtual:
		balance := cfg.AccountBalance()
		executor = bcs.NewVirtualExecutor(balance)
		logx.Mode(true, fmt.Sprintf("баланс %.0f руб.", balance))

	case config.TradingModeReal:
		if len(experiments) != 1 {
			logx.Fatal("real mode: ожидается ровно один эксперимент")
		}
		client.SetWriteMode()
		if err := client.Connect(ctx); err != nil {
			logx.Fatalf("авторизация (write) провалена: %v", err)
		}
		executor = client
		logx.Mode(false, "BCSClient (trade-api-write)")

		balance, err := client.GetBalance(ctx)
		if err != nil {
			logx.Warn("не удалось получить баланс: %v", err)
		} else {
			logx.Info("Баланс счёта: %.2f руб.", balance)
		}

	default:
		logx.Fatalf("неизвестный trading_mode: %q", cfg.TradingMode)
	}

	feed := datafeed.New(client)
	strategyCount := 0
	hub := live.NewHub()
	hubFeeds := make(map[[2]string]bool) // (ticker, timeframe) → hub уже подписан

	for _, exp := range experiments {
		expTickers := cfg.TickersForExperiment(exp)
		session := cfg.SessionForExperiment(exp)

		// Эффективный таймфрейм эксперимента: exp.CandleTimeframe уже
		// нормализован в ResolvedExperiments() (fallback на cfg.CandleTimeFrame,
		// если в конфиге эксперимента не задан свой) — см. ADR 0001.
		timeframe := exp.CandleTimeframe
		if timeframe == "" {
			timeframe = cfg.CandleTimeFrame
		}

		for _, tc := range expTickers {
			// Каждый (эксперимент × тикер) — самодостаточная стратегия
			// (internal/strategies/adapter.SelfManagedStrategy): сама ведёт
			// SL/TP/трейлинг/EOD/сайзинг. Каркас (StrategyRunner) даёт ей
			// поток данных, OrderExecutor, портфельный риск и TradeStore.
			label := fmt.Sprintf("strategy/%s/%s", exp.ID, tc.Symbol)

			sessionClock, err := engine.NewSessionClockExt(
				session.Timezone, session.EODCloseTime, session.SessionOpenTime,
				session.EntryDelayMinutes, session.WeekdaysOnly, session.WeekendOnly,
			)
			if err != nil {
				logx.Fatalf("ошибка создания SessionClock %s: %v", label, err)
			}

			signalStrategy, err := exp.Strategy.BuildStrategy(session)
			if err != nil {
				logx.Fatalf("ошибка создания сигнальной стратегии %s: %v", label, err)
			}

			step := tc.StepPriceValue
			if step <= 0 {
				step = 1.0
			}
			trailCfg := exp.Strategy.TrailingConfig(step, cfg.CostsConfig(), cfg.ClassCode)

			selfManaged := adapter.New(adapter.Config{
				Signal:          signalStrategy,
				Label:           label,
				Ticker:          tc.Symbol,
				ExperimentID:    exp.ID,
				StopMode:        exp.Strategy.StopMode,
				StepPriceValue:  step,
				TradingMode:     cfg.TradingMode,
				RunID:           runID,
				ClassCode:       cfg.ClassCode,
				CandleTimeframe: timeframe,
				Lookback:        exp.Strategy.Lookback,
				RiskPerTradePct: accountRisk.RiskPerTradePercent,
				Deposit:         accountRisk.Deposit,
				MaxDailyLoss:    accountRisk.MaxDailyLoss,
				TrailCfg:        trailCfg,
				CostsCfg:        cfg.CostsConfig(),
				RewardRatio:     exp.Strategy.EffectiveRewardRatio(),
				MaxTradesPerDay: exp.Strategy.MaxTradesPerTickerPerDay,
				Session:         sessionClock,
			})

			hub.Register(selfManaged)

			candleCh := make(chan models.Candle, 64)
			tickCh := make(chan models.Tick, 256)
			runner := engine.NewStrategyRunner(selfManaged, tc.Symbol, timeframe, candleCh, tickCh, executor, globalRisk, tradeStore, sessionClock)
			if err := feed.Subscribe(tc.Symbol, timeframe, candleCh, tickCh); err != nil {
				logx.Fatalf("ошибка подписки на данные %s (%s): %v", label, timeframe, err)
			}
			go runner.Start(ctx)
			strategyCount++

			// live-дашборд: отдельный consumer того же (ticker, timeframe)
			// через fan-out DataFeed — свечи/тики в hub для /positions,
			// /candles, /chart. Один на пару, не на каждый эксперимент.
			if key := [2]string{tc.Symbol, timeframe}; !hubFeeds[key] {
				hubFeeds[key] = true
				hubCandleCh := make(chan models.Candle, 128)
				hubTickCh := make(chan models.Tick, 256)
				if err := feed.Subscribe(tc.Symbol, timeframe, hubCandleCh, hubTickCh); err != nil {
					logx.Fatalf("ошибка подписки hub %s (%s): %v", label, timeframe, err)
				}
				go ingestMarketToHub(ctx, hub, tc.Symbol, hubCandleCh, hubTickCh)
			}
		}
	}

	logx.Info(
		"Шаг 2: Запущено %d стратегий (%d экспериментов, EOD: %s МСК)",
		strategyCount, len(experiments), cfg.Session.EODCloseTime,
	)

	if *httpListen != "" {
		deposit := cfg.AccountBalance()
		adminToken := strings.TrimSpace(os.Getenv("ADMIN_TOKEN"))
		liveSrv, err := live.NewServer(hub, live.Options{
			Listen:   *httpListen,
			Token:    adminToken,
			Deposit:  deposit,
			Exec:     executor,
			Reader:   tradeReader,
			Archives: api.NewArchiveStore(*archivesPath),
			Candles:  live.NewCachedDayCandles(&live.BCSCandleFetcher{Client: client}, cfg.ClassCode, 0),
		})
		if err != nil {
			logx.Fatalf("HTTP UI/API: %v", err)
		}
		httpSrv := &http.Server{
			Addr:              liveSrv.ListenAddr(),
			Handler:           liveSrv.Handler(),
			ReadHeaderTimeout: 5 * time.Second,
			ReadTimeout:       10 * time.Second,
			WriteTimeout:      60 * time.Second,
		}
		go func() {
			authHint := "без токена (только localhost)"
			if adminToken != "" {
				authHint = "ADMIN_TOKEN задан"
			}
			logx.Info("Админка: http://%s (%s)", liveSrv.ListenAddr(), authHint)
			if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				logx.Error("HTTP UI/API: %v", err)
			}
		}()
		go func() {
			<-ctx.Done()
			shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancelShutdown()
			_ = httpSrv.Shutdown(shutdownCtx)
		}()
	}

	go func() {
		if err := feed.Run(ctx); err != nil && ctx.Err() == nil {
			logx.Error("стрим рыночных данных остановлен: %v", err)
			cancel() // даём main нормально завершиться через <-ctx.Done()
		}
	}()

	logx.Info("Шаг 3: Торговый цикл активен. Мониторинг SL/TP и EOD включён.")

	<-ctx.Done()
	logx.Info("Завершение работы...")
}

// ingestMarketToHub перекладывает свечи/тики из fan-out DataFeed в live.Hub
// (буфер дня + last price для /positions, /candles, /chart). Отдельный
// consumer, не влияет на канал стратегии.
func ingestMarketToHub(
	ctx context.Context,
	hub *live.Hub,
	ticker string,
	candleCh <-chan models.Candle,
	tickCh <-chan models.Tick,
) {
	for {
		select {
		case <-ctx.Done():
			return
		case c, ok := <-candleCh:
			if !ok {
				return
			}
			if c.Ticker == "" {
				c.Ticker = ticker
			}
			hub.IngestCandle(c)
		case t, ok := <-tickCh:
			if !ok {
				return
			}
			if t.Ticker == "" {
				t.Ticker = ticker
			}
			hub.IngestTick(t)
		}
	}
}

func runSmokeTest(ctx context.Context, cfg *config.Config, client *bcs.BCSClient) {
	if cfg.TradingMode != config.TradingModeVirtual {
		logx.Fatal("smoke-test: только trading_mode: virtual")
	}

	tickers := cfg.AllTickerSymbols()
	if len(tickers) == 0 {
		logx.Fatal("smoke-test: нет тикеров в конфиге")
	}
	ticker := tickers[0]

	executor := bcs.NewVirtualExecutor(cfg.AccountBalance())

	if err := engine.RunSmokeTest(ctx, client, ticker, executor); err != nil {
		logx.Fatalf("smoke-test провален: %v", err)
	}
}
