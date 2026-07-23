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
	"bcs-trading-bot/internal/engine"
	"bcs-trading-bot/internal/live"
	"bcs-trading-bot/internal/risk"
	"bcs-trading-bot/internal/storage/sqlite"
	"bcs-trading-bot/pkg/interfaces"
	"bcs-trading-bot/pkg/logx"
	"bcs-trading-bot/pkg/models"
)

func main() {
	configPath := flag.String("config", "configs/runs/portfolio-paper.yaml", "путь к YAML-конфигу")
	noColor := flag.Bool("no-color", false, "отключить цветной вывод в терминале")
	smokeTest := flag.Bool("smoke-test", false, "быстрая проверка: OAuth + WebSocket + виртуальная сделка без записи в БД")
	httpListen := flag.String("http-listen", "127.0.0.1:8091", "адрес HTTP UI/API админки (пустая строка — отключить)")
	archivesPath := flag.String("archives", "data/archives.json", "путь к JSON с архивами периодов")
	flag.Parse()

	if *noColor {
		logx.SetColorEnabled(false)
	}

	logx.Info("Запуск торгового робота БКС на Go...")

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
	pct := accountRisk.MaxDailyLossPercent
	if pct <= 0 {
		pct = 2.0
	}
	globalRisk := risk.NewGlobalRiskController(accountRisk.Deposit, pct, maxParallel)
	logx.Info(
		"Единый счёт: депозит %.0f | CB %.1f%% | max_parallel=%d | one-position-per-ticker",
		accountRisk.Deposit, pct, maxParallel,
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

	routes := make(map[string][]bcs.WorkerRoutes)
	workerCount := 0
	hub := live.NewHub()

	for _, exp := range experiments {
		expTickers := cfg.TickersForExperiment(exp)
		session := cfg.SessionForExperiment(exp)

		workerExp := exp
		workerExp.Risk = accountRisk

		for _, tc := range expTickers {
			worker, err := engine.NewTickerWorker(
				tc.Symbol,
				workerExp,
				tc.StepPriceValue,
				cfg.CostsConfig(),
				session,
				cfg.TradingMode,
				runID,
				cfg.ClassCode,
				cfg.CandleTimeFrame,
				tradeStore,
				globalRisk,
			)
			if err != nil {
				logx.Fatalf("ошибка создания воркера %s/%s: %v", exp.ID, tc.Symbol, err)
			}

			hub.Register(worker)
			candleIn, tickIn := teeMarketToHub(ctx, hub, tc.Symbol, worker.CandleChan(), worker.TickChan())
			routes[tc.Symbol] = append(routes[tc.Symbol], bcs.WorkerRoutes{
				CandleChan: candleIn,
				TickChan:   tickIn,
			})
			go worker.Start(ctx, executor)
			workerCount++
		}
	}

	logx.Info(
		"Шаг 2: Запущено %d воркеров (%d экспериментов, EOD: %s МСК)",
		workerCount, len(experiments), cfg.Session.EODCloseTime,
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
		if err := client.SubscribeMarketDataFanOut(ctx, routes); err != nil && ctx.Err() == nil {
			logx.Error("стрим рыночных данных остановлен: %v", err)
			cancel() // даём main нормально завершиться через <-ctx.Done()
		}
	}()

	logx.Info("Шаг 3: Торговый цикл активен. Мониторинг SL/TP и EOD включён.")

	<-ctx.Done()
	logx.Info("Завершение работы...")
}

// teeMarketToHub дублирует свечи/тики в live hub и в каналы воркера.
func teeMarketToHub(
	ctx context.Context,
	hub *live.Hub,
	ticker string,
	candleOut chan<- models.Candle,
	tickOut chan<- models.Tick,
) (chan<- models.Candle, chan<- models.Tick) {
	candleIn := make(chan models.Candle, 64)
	tickIn := make(chan models.Tick, 256)

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case c, ok := <-candleIn:
				if !ok {
					return
				}
				if c.Ticker == "" {
					c.Ticker = ticker
				}
				hub.IngestCandle(c)
				select {
				case candleOut <- c:
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case t, ok := <-tickIn:
				if !ok {
					return
				}
				if t.Ticker == "" {
					t.Ticker = ticker
				}
				hub.IngestTick(t)
				select {
				case tickOut <- t:
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	return candleIn, tickIn
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
