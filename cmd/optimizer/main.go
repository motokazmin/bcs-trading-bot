package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"bcs-trading-bot/internal/bcs"
	"bcs-trading-bot/internal/optimizer"
	"bcs-trading-bot/internal/strategy"
	"bcs-trading-bot/pkg/logx"
	"bcs-trading-bot/pkg/models"
)

const defaultUniverse = "config/optimizer/universe.yaml"

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "run":
		runCmd(os.Args[2:])
	case "backtest":
		backtestCmd(os.Args[2:])
	case "sync-history":
		syncHistoryCmd(os.Args[2:])
	case "fetch-history":
		// legacy: полная перезагрузка диапазона
		fetchHistoryCmd(os.Args[2:])
	case "-h", "--help", "help":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "неизвестная подкоманда: %s\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println(`bcs-strategy-optimizer — offline подбор гиперпараметров

Usage:
  optimizer sync-history [flags]   инкрементальная догрузка до текущего момента
  optimizer run [flags]          walk-forward оптимизация
  optimizer backtest [flags]     один прогон стратегии на истории
  optimizer fetch-history [flags] полная перезагрузка диапазона (legacy)

Подробности: cmd/optimizer/README.md (подход, флаги, scoring)`)
}

func runCmd(args []string) {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	universePath := fs.String("universe", defaultUniverse, "YAML со списком инструментов")
	tickers := fs.String("tickers", "", "override тикеров через запятую")
	historyDir := fs.String("history-dir", "data/history", "директория CSV-истории")
	strategyID := fs.String("strategy", strategy.DefaultType(), "id стратегии: "+strings.Join(strategy.ListIDs(), ", "))
	searchSpace := fs.String("search-space", "", "YAML search space (default: из стратегии)")
	dateFrom := fs.String("date-from", "", "начало периода YYYY-MM-DD (default: из CSV)")
	dateTo := fs.String("date-to", "", "конец периода YYYY-MM-DD (default: из CSV)")
	trainMonths := fs.Int("train-months", 6, "длина train-окна в месяцах")
	testMonths := fs.Int("test-months", 2, "длина test-окна в месяцах")
	stepMonths := fs.Int("step-months", 1, "шаг сдвига окна в месяцах")
	trials := fs.Int("trials", 200, "число random search trials")
	minTrades := fs.Int("min-trades", 20, "мин. сделок для валидного score")
	commission := fs.Float64("commission-per-trade", 5.0, "комиссия round-trip за лот, руб")
	stopMode := fs.String("stop-mode", "atr", "stop_mode: range или atr")
	deposit := fs.Float64("deposit", 200000, "депозит для risk manager")
	stepPrice := fs.Float64("step-price-value", 1.0, "стоимость шага цены")
	output := fs.String("output", "results/", "директория для результатов")
	seed := fs.Int64("seed", time.Now().UnixNano(), "seed для random search")
	parallel := fs.Int("parallel", 0, "параллельных trials (0 = NumCPU)")
	twoPhase := fs.Bool("two-phase", false, "двухфазный поиск: random search на lean, финал на полном universe")
	phase1Tickers := fs.String("phase1-tickers", "", "тикеры фазы 1 (default: lean_tickers из universe)")
	phase2Top := fs.Int("phase2-top", 20, "сколько лучших конфигов фазы 1 пересчитать на полном universe")
	_ = fs.Parse(args)

	spacePath := *searchSpace
	if spacePath == "" {
		var err error
		spacePath, err = strategy.DefaultSearchSpacePath(*strategyID)
		if err != nil {
			logx.Fatalf("search-space: %v", err)
		}
	}

	u, err := optimizer.LoadUniverse(*universePath)
	if err != nil {
		logx.Fatalf("universe: %v", err)
	}
	tickerList := u.ResolveTickers(*tickers)

	space, err := optimizer.LoadSearchSpace(spacePath)
	if err != nil {
		logx.Fatalf("search space: %v", err)
	}
	if err := optimizer.ValidateSearchSpaceStrategy(*strategyID, space); err != nil {
		logx.Fatalf("%v", err)
	}

	candleData, err := optimizer.LoadCandleData(*historyDir, tickerList)
	if err != nil {
		logx.Fatalf("загрузка истории: %v", err)
	}
	tickerList = filterLoadedTickers(tickerList, candleData)

	from, to, err := resolveDateRange(*dateFrom, *dateTo, candleData)
	if err != nil {
		logx.Fatalf("даты: %v", err)
	}

	windows := optimizer.GenerateWindows(from, to, *trainMonths, *testMonths, *stepMonths)
	if len(windows) == 0 {
		logx.Fatal("не удалось сгенерировать walk-forward окна — проверьте даты и train/test months")
	}
	logx.Info("стратегия: %s | тикеры: %s | период: %s → %s | окон: %d | trials: %d",
		*strategyID, strings.Join(tickerList, ","), from.Format("2006-01-02"), to.Format("2006-01-02"), len(windows), *trials)

	settings := optimizer.RunSettings{
		Tickers:            tickerList,
		HistoryDir:         *historyDir,
		StrategyID:         *strategyID,
		StopMode:           *stopMode,
		ClassCode:          u.ClassCode,
		CandleTimeframe:    u.CandleTimeframe,
		Deposit:            *deposit,
		StepPriceValue:     *stepPrice,
		CommissionPerTrade: *commission,
		MinTrades:          *minTrades,
		Session:            optimizer.DefaultSession(),
	}

	evaluator := optimizer.NewEvaluator(settings, space, candleData)
	evaluator.PrecomputeWindowSlices(windows)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	optCfg := optimizer.OptimizationConfig{
		Trials:      *trials,
		MinTrades:   *minTrades,
		Seed:        *seed,
		Parallelism: *parallel,
	}

	var result *optimizer.RunResult
	if *twoPhase {
		phase1, err := optimizer.ResolvePhase1Tickers(*phase1Tickers, u.LeanTickers, tickerList)
		if err != nil {
			logx.Fatalf("two-phase: %v", err)
		}
		logx.Info("two-phase: включён | фаза 1: %s | top %d → полный universe", strings.Join(phase1, ","), *phase2Top)
		result = optimizer.RunTwoPhaseOptimization(ctx, settings, space, candleData, windows, optimizer.TwoPhaseConfig{
			OptimizationConfig: optCfg,
			Phase1Tickers:      phase1,
			Phase2Top:          *phase2Top,
		})
	} else {
		result = optimizer.RunOptimizationWithConfig(ctx, evaluator, space, windows, optCfg)
	}

	jsonPath, yamlPath, err := optimizer.WriteResults(*output, result, settings, space)
	if err != nil {
		logx.Fatalf("запись результатов: %v", err)
	}

	optimizer.PrintTopN(result, 5)
	logx.Info("JSON: %s", jsonPath)
	logx.Info("best-config: %s", yamlPath)
}

func backtestCmd(args []string) {
	fs := flag.NewFlagSet("backtest", flag.ExitOnError)
	universePath := fs.String("universe", defaultUniverse, "YAML со списком инструментов")
	tickers := fs.String("tickers", "", "override тикеров через запятую")
	historyDir := fs.String("history-dir", "data/history", "директория CSV-истории")
	strategyID := fs.String("strategy", strategy.DefaultType(), "id стратегии")
	searchSpace := fs.String("search-space", "", "YAML search space (default: из стратегии)")
	dateFrom := fs.String("date-from", "", "начало периода YYYY-MM-DD")
	dateTo := fs.String("date-to", "", "конец периода YYYY-MM-DD")
	commission := fs.Float64("commission-per-trade", 5.0, "комиссия round-trip за лот, руб")
	stopMode := fs.String("stop-mode", "atr", "stop_mode: range или atr")
	deposit := fs.Float64("deposit", 200000, "депозит")
	stepPrice := fs.Float64("step-price-value", 1.0, "стоимость шага цены")
	_ = fs.Parse(args)

	spacePath := *searchSpace
	if spacePath == "" {
		var err error
		spacePath, err = strategy.DefaultSearchSpacePath(*strategyID)
		if err != nil {
			logx.Fatalf("search-space: %v", err)
		}
	}

	u, err := optimizer.LoadUniverse(*universePath)
	if err != nil {
		logx.Fatalf("universe: %v", err)
	}
	space, err := optimizer.LoadSearchSpace(spacePath)
	if err != nil {
		logx.Fatalf("search space: %v", err)
	}
	if err := optimizer.ValidateSearchSpaceStrategy(*strategyID, space); err != nil {
		logx.Fatalf("%v", err)
	}

	tickerList := u.ResolveTickers(*tickers)
	candleData, err := optimizer.LoadCandleData(*historyDir, tickerList)
	if err != nil {
		logx.Fatalf("загрузка истории: %v", err)
	}
	tickerList = filterLoadedTickers(tickerList, candleData)

	from, to, err := resolveDateRange(*dateFrom, *dateTo, candleData)
	if err != nil {
		logx.Fatalf("даты: %v", err)
	}

	settings := optimizer.RunSettings{
		Tickers:            tickerList,
		StrategyID:         *strategyID,
		StopMode:           *stopMode,
		ClassCode:          u.ClassCode,
		CandleTimeframe:    u.CandleTimeframe,
		Deposit:            *deposit,
		StepPriceValue:     *stepPrice,
		CommissionPerTrade: *commission,
		MinTrades:          1,
		Session:            optimizer.DefaultSession(),
	}
	evaluator := optimizer.NewEvaluator(settings, space, candleData)
	params := optimizer.DefaultParamsFromSpace(space)

	ctx := context.Background()
	result := optimizer.RunBacktest(ctx, evaluator, from, to, params)

	fmt.Printf("strategy=%s period=%s → %s\n", result.StrategyID, from.Format("2006-01-02"), to.Format("2006-01-02"))
	fmt.Printf("trades=%d total_pnl=%.2f sharpe=%.4f max_dd=%.2f win_rate=%.2f\n",
		result.NumTrades, result.Metrics.TotalPnL, result.Metrics.Sharpe,
		result.Metrics.MaxDrawdown, result.Metrics.WinRate)
}

func syncHistoryCmd(args []string) {
	fs := flag.NewFlagSet("sync-history", flag.ExitOnError)
	universePath := fs.String("universe", defaultUniverse, "YAML со списком инструментов")
	tickers := fs.String("tickers", "", "override тикеров через запятую")
	outputDir := fs.String("output-dir", "data/history", "директория для CSV")
	initialYears := fs.Int("initial-years", 0, "глубина первичной загрузки (0 = из universe)")
	chunkDelay := fs.Duration("chunk-delay", 50*time.Millisecond, "мин. пауза между чанками (adaptive) или фиксированная")
	maxChunkDelay := fs.Duration("max-chunk-delay", 3*time.Second, "макс. пауза adaptive throttle")
	tickerDelay := fs.Duration("ticker-delay", 3*time.Second, "пауза между тикерами (последовательный режим)")
	adaptiveDelay := fs.Bool("adaptive-delay", true, "адаптивная пауза: быстрее при успехе, медленнее при 429")
	parallelTickers := fs.Int("parallel-tickers", 5, "параллельная загрузка тикеров (общий rate limiter)")
	_ = fs.Parse(args)

	token := os.Getenv("BCS_REFRESH_TOKEN")
	if token == "" {
		logx.Fatal("задайте BCS_REFRESH_TOKEN")
	}

	u, err := optimizer.LoadUniverse(*universePath)
	if err != nil {
		logx.Fatalf("universe: %v", err)
	}
	tickerList := u.ResolveTickers(*tickers)
	logx.Info("sync-history: %d инструментов (%s)", len(tickerList), strings.Join(tickerList, ","))

	years := u.InitialHistoryYears
	if *initialYears > 0 {
		years = *initialYears
	}

	client := bcs.NewBCSClient(token)
	client.SetClassCode(u.ClassCode)
	client.SetCandleTimeFrame(u.CandleTimeframe)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if err := client.Connect(ctx); err != nil {
		logx.Fatalf("авторизация: %v", err)
	}

	if err := optimizer.SyncHistory(ctx, client, optimizer.SyncOptions{
		OutputDir:           *outputDir,
		ClassCode:           u.ClassCode,
		TimeFrame:           u.CandleTimeframe,
		InitialHistoryYears: years,
		Tickers:             tickerList,
		ParallelTickers:     *parallelTickers,
		Fetch: optimizer.FetchConfig{
			ChunkDelay:    *chunkDelay,
			MaxChunkDelay: *maxChunkDelay,
			TickerDelay:   *tickerDelay,
			Adaptive:      *adaptiveDelay,
		},
	}); err != nil {
		logx.Fatalf("sync-history: %v", err)
	}
}

func fetchHistoryCmd(args []string) {
	fs := flag.NewFlagSet("fetch-history", flag.ExitOnError)
	universePath := fs.String("universe", defaultUniverse, "YAML со списком инструментов")
	tickers := fs.String("tickers", "", "override тикеров")
	dateFrom := fs.String("date-from", "", "начало YYYY-MM-DD (default: initial_history_years назад)")
	dateTo := fs.String("date-to", "", "конец YYYY-MM-DD (default: сегодня)")
	outputDir := fs.String("output-dir", "data/history", "директория для CSV")
	_ = fs.Parse(args)

	token := os.Getenv("BCS_REFRESH_TOKEN")
	if token == "" {
		logx.Fatal("задайте BCS_REFRESH_TOKEN")
	}

	u, err := optimizer.LoadUniverse(*universePath)
	if err != nil {
		logx.Fatalf("universe: %v", err)
	}
	tickerList := u.ResolveTickers(*tickers)

	now := time.Now()
	from := now.AddDate(-u.InitialHistoryYears, 0, 0)
	to := now
	if *dateFrom != "" {
		from, err = optimizer.ParseDate(*dateFrom)
		if err != nil {
			logx.Fatalf("date-from: %v", err)
		}
	}
	if *dateTo != "" {
		to, err = optimizer.ParseDate(*dateTo)
		if err != nil {
			logx.Fatalf("date-to: %v", err)
		}
	}

	client := bcs.NewBCSClient(token)
	client.SetClassCode(u.ClassCode)
	client.SetCandleTimeFrame(u.CandleTimeframe)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if err := client.Connect(ctx); err != nil {
		logx.Fatalf("авторизация: %v", err)
	}

	logx.Info("fetch-history (полная перезагрузка): %s → %s", from.Format("2006-01-02"), to.Format("2006-01-02"))
	if err := optimizer.FetchHistory(ctx, client, tickerList, u.ClassCode, u.CandleTimeframe, *outputDir, from, to, optimizer.DefaultFetchConfig()); err != nil {
		logx.Fatalf("fetch-history: %v", err)
	}
}

func filterLoadedTickers(requested []string, data map[string][]models.Candle) []string {
	out := make([]string, 0, len(data))
	for _, ticker := range requested {
		if _, ok := data[ticker]; ok {
			out = append(out, ticker)
		}
	}
	return out
}

func resolveDateRange(fromStr, toStr string, data map[string][]models.Candle) (time.Time, time.Time, error) {
	if fromStr != "" && toStr != "" {
		from, err := optimizer.ParseDate(fromStr)
		if err != nil {
			return time.Time{}, time.Time{}, err
		}
		to, err := optimizer.ParseDate(toStr)
		if err != nil {
			return time.Time{}, time.Time{}, err
		}
		return from, to, nil
	}

	csvFrom, csvTo, ok := optimizer.CandleDataRange(data)
	if !ok {
		return time.Time{}, time.Time{}, fmt.Errorf("нет данных в CSV")
	}

	from := csvFrom
	to := csvTo
	if fromStr != "" {
		var err error
		from, err = optimizer.ParseDate(fromStr)
		if err != nil {
			return time.Time{}, time.Time{}, err
		}
	}
	if toStr != "" {
		var err error
		to, err = optimizer.ParseDate(toStr)
		if err != nil {
			return time.Time{}, time.Time{}, err
		}
	}
	return from, to, nil
}
