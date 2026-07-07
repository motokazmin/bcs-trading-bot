package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"bcs-trading-bot/internal/bcs"
	"bcs-trading-bot/internal/config"
	"bcs-trading-bot/internal/engine"
	"bcs-trading-bot/internal/storage/sqlite"
	"bcs-trading-bot/pkg/interfaces"
	"bcs-trading-bot/pkg/logx"
)

func main() {
	configPath := flag.String("config", "configs/experiments-all.yaml", "путь к YAML-конфигу")
	noColor := flag.Bool("no-color", false, "отключить цветной вывод в терминале")
	smokeTest := flag.Bool("smoke-test", false, "быстрая проверка: OAuth + WebSocket + виртуальная сделка без записи в БД")
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
	if cfg.StorageEnabled() {
		store, err := sqlite.Open(cfg.Storage.Path)
		if err != nil {
			logx.Fatalf("ошибка открытия БД сделок: %v", err)
		}
		tradeStore = store
		defer func() {
			if err := store.Close(); err != nil {
				logx.Warn("ошибка закрытия БД: %v", err)
			}
		}()
		logx.Info("Хранилище сделок: %s", cfg.Storage.Path)
	}

	runID := fmt.Sprintf("%s-%s", filepath.Base(*configPath), time.Now().Format("20060102-150405"))

	executors := make(map[string]interfaces.OrderExecutor, len(experiments))
	switch cfg.TradingMode {
	case config.TradingModeVirtual:
		for _, exp := range experiments {
			executors[exp.ID] = bcs.NewVirtualExecutor(exp.Virtual.Balance)
			logx.Mode(true, fmt.Sprintf("[%s] баланс %.0f руб.", exp.ID, exp.Virtual.Balance))
		}

	case config.TradingModeReal:
		if len(experiments) != 1 {
			logx.Fatal("real mode: ожидается ровно один эксперимент")
		}
		client.SetWriteMode()
		if err := client.Connect(ctx); err != nil {
			logx.Fatalf("авторизация (write) провалена: %v", err)
		}
		executors[experiments[0].ID] = client
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

	for _, exp := range experiments {
		executor := executors[exp.ID]
		expTickers := cfg.TickersForExperiment(exp)
		session := cfg.SessionForExperiment(exp)
		tickerCount := len(expTickers)

		for _, tc := range expTickers {
			worker, err := engine.NewTickerWorker(
				tc.Symbol,
				exp,
				tickerCount,
				tc.StepPriceValue,
				cfg.CommissionPerLot(),
				session,
				cfg.TradingMode,
				runID,
				cfg.ClassCode,
				cfg.CandleTimeFrame,
				tradeStore,
			)
			if err != nil {
				logx.Fatalf("ошибка создания воркера %s/%s: %v", exp.ID, tc.Symbol, err)
			}

			routes[tc.Symbol] = append(routes[tc.Symbol], bcs.WorkerRoutes{
				CandleChan: worker.CandleChan(),
				TickChan:   worker.TickChan(),
			})
			go worker.Start(ctx, executor)
			workerCount++
		}
	}

	logx.Info(
		"Шаг 2: Запущено %d воркеров (%d экспериментов, EOD: %s МСК)",
		workerCount, len(experiments), cfg.Session.EODCloseTime,
	)

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

func runSmokeTest(ctx context.Context, cfg *config.Config, client *bcs.BCSClient) {
	if cfg.TradingMode != config.TradingModeVirtual {
		logx.Fatal("smoke-test: только trading_mode: virtual")
	}

	tickers := cfg.AllTickerSymbols()
	if len(tickers) == 0 {
		logx.Fatal("smoke-test: нет тикеров в конфиге")
	}
	ticker := tickers[0]

	executor := bcs.NewVirtualExecutor(cfg.Risk.Deposit)
	if cfg.HasExperiments() {
		exp := cfg.ResolvedExperiments()[0]
		executor = bcs.NewVirtualExecutor(exp.Virtual.Balance)
	}

	if err := engine.RunSmokeTest(ctx, client, ticker, executor); err != nil {
		logx.Fatalf("smoke-test провален: %v", err)
	}
}
