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
	"bcs-trading-bot/internal/optimizer/charts"
	"bcs-trading-bot/internal/optimizer/core"
	"bcs-trading-bot/internal/optimizer/data"
	"bcs-trading-bot/internal/optimizer/eval"
	"bcs-trading-bot/internal/optimizer/report"
	"bcs-trading-bot/internal/strategy"
	"bcs-trading-bot/pkg/logx"
	"bcs-trading-bot/pkg/models"
)

const defaultTickersConfig = "configs/shared/tickers.yaml"

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
	case "charts":
		chartsCmd(os.Args[2:])
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
  optimizer charts [flags]     графики сделок по эксперименту и тикеру
  optimizer fetch-history [flags] полная перезагрузка диапазона (legacy)

Подробности: cmd/optimizer/README.md (подход, флаги, scoring)`)
}

func runCmd(args []string) {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	tickersConfigPath := fs.String("tickers-config", defaultTickersConfig, "YAML со списком инструментов")
	tickers := fs.String("tickers", "", "override тикеров через запятую")
	historyDir := fs.String("history-dir", "data/history", "директория CSV-истории")
	strategyID := fs.String("strategy", strategy.DefaultType(), "id стратегии: "+strings.Join(strategy.ListIDs(), ", "))
	searchSpace := fs.String("search-space", "", "YAML search space (default: из стратегии)")
	dateFrom := fs.String("date-from", "", "начало периода YYYY-MM-DD (default: из CSV)")
	dateTo := fs.String("date-to", "", "конец периода YYYY-MM-DD (default: из CSV)")
	windowMonths := fs.Int("window-months", 2, "длина окна оценки в месяцах")
	stepMonths := fs.Int("step-months", 1, "шаг сдвига окна в месяцах")
	trials := fs.Int("trials", 200, "число random search trials")
	minTrades := fs.Int("min-trades", 20, "мин. сделок для валидного score")
	commission := fs.Float64("commission-per-lot", -1, "комиссия round-trip за акцию/контракт, руб (<=0: из tickers-config или class_code)")
	stopMode := fs.String("stop-mode", "atr", "stop_mode: range или atr")
	deposit := fs.Float64("deposit", 200000, "депозит для risk manager")
	stepPrice := fs.Float64("step-price-value", 1.0, "стоимость шага цены")
	output := fs.String("output", "results/", "директория для результатов")
	seed := fs.Int64("seed", time.Now().UnixNano(), "seed для random search")
	parallel := fs.Int("parallel", 0, "параллельных trials (0 = NumCPU)")
	twoPhase := fs.Bool("two-phase", false, "двухфазный поиск: random search на lean, финал на полном universe")
	phase1Tickers := fs.String("phase1-tickers", "", "тикеры фазы 1 (default: lean_tickers из tickers-config)")
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

	u, err := data.LoadTickersConfig(*tickersConfigPath)
	if err != nil {
		logx.Fatalf("tickers-config: %v", err)
	}
	tickerList := u.ResolveTickers(*tickers)

	space, err := core.LoadSearchSpace(spacePath)
	if err != nil {
		logx.Fatalf("search space: %v", err)
	}
	if err := eval.ValidateSearchSpaceStrategy(*strategyID, space); err != nil {
		logx.Fatalf("%v", err)
	}

	candleData, err := eval.LoadCandleData(*historyDir, tickerList)
	if err != nil {
		logx.Fatalf("загрузка истории: %v", err)
	}
	tickerList = filterLoadedTickers(tickerList, candleData)

	from, to, err := resolveDateRange(*dateFrom, *dateTo, candleData)
	if err != nil {
		logx.Fatalf("даты: %v", err)
	}

	windows := core.GenerateWindows(from, to, *windowMonths, *stepMonths)
	if len(windows) == 0 {
		logx.Fatal("не удалось сгенерировать walk-forward окна — проверьте даты и window-months")
	}
	logx.Info("стратегия: %s | тикеры: %s | период: %s → %s | окон: %d | trials: %d",
		*strategyID, strings.Join(tickerList, ","), from.Format("2006-01-02"), to.Format("2006-01-02"), len(windows), *trials)

	settings := eval.RunSettings{
		Tickers:            tickerList,
		HistoryDir:         *historyDir,
		StrategyID:         *strategyID,
		StopMode:           *stopMode,
		ClassCode:          u.ClassCode,
		CandleTimeframe:    u.CandleTimeframe,
		Deposit:            *deposit,
		StepPriceValue:     *stepPrice,
		CommissionPerLot:     u.CommissionPerLot(*commission),
		MinTrades:          *minTrades,
		Session:            optimizer.DefaultSession(),
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	optCfg := eval.OptimizationConfig{
		Trials:      *trials,
		MinTrades:   *minTrades,
		Seed:        *seed,
		Parallelism: *parallel,
	}

	var result *eval.RunResult
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
		// PrecomputeWindowSlices нарезает свечи по всем окнам сразу для полного
		// universe — нужно только в этой ветке; в -two-phase свои evaluator'ы
		// создаёт и нарезает RunTwoPhaseOptimization (сначала на lean, потом на
		// полном), пересоздавать их здесь заранее было бы лишней работой.
		evaluator := eval.NewEvaluator(settings, space, candleData)
		evaluator.PrecomputeWindowSlices(windows)
		result = eval.RunOptimizationWithConfig(ctx, evaluator, space, windows, optCfg)
	}

	jsonPath, yamlPath, err := report.WriteResults(*output, result, settings, space)
	if err != nil {
		logx.Fatalf("запись результатов: %v", err)
	}

	report.PrintTopN(result, 5)
	logx.Info("JSON: %s", jsonPath)
	logx.Info("best-config: %s", yamlPath)

	chartsResult, chartsErr := charts.RunCharts(ctx, charts.ChartsOptions{
		ExperimentDir:      *output,
		HistoryDir:         *historyDir,
		CommissionPerLot:     u.CommissionPerLot(*commission),
	})
	if chartsErr != nil {
		logx.Warn("charts: %v", chartsErr)
	} else {
		logx.Info("charts: %d files → %s", len(chartsResult.Files), chartsResult.ChartsDir)
		if chartsResult.ExportDir != "" {
			logx.Info("export: %s", chartsResult.ExportDir)
		}
	}
}

func backtestCmd(args []string) {
	fs := flag.NewFlagSet("backtest", flag.ExitOnError)
	tickersConfigPath := fs.String("tickers-config", defaultTickersConfig, "YAML со списком инструментов")
	tickers := fs.String("tickers", "", "override тикеров через запятую")
	historyDir := fs.String("history-dir", "data/history", "директория CSV-истории")
	strategyID := fs.String("strategy", strategy.DefaultType(), "id стратегии")
	searchSpace := fs.String("search-space", "", "YAML search space (default: из стратегии)")
	dateFrom := fs.String("date-from", "", "начало периода YYYY-MM-DD")
	dateTo := fs.String("date-to", "", "конец периода YYYY-MM-DD")
	commission := fs.Float64("commission-per-lot", -1, "комиссия round-trip за акцию/контракт, руб (<=0: из tickers-config или class_code)")
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

	u, err := data.LoadTickersConfig(*tickersConfigPath)
	if err != nil {
		logx.Fatalf("tickers-config: %v", err)
	}
	space, err := core.LoadSearchSpace(spacePath)
	if err != nil {
		logx.Fatalf("search space: %v", err)
	}
	if err := eval.ValidateSearchSpaceStrategy(*strategyID, space); err != nil {
		logx.Fatalf("%v", err)
	}

	tickerList := u.ResolveTickers(*tickers)
	candleData, err := eval.LoadCandleData(*historyDir, tickerList)
	if err != nil {
		logx.Fatalf("загрузка истории: %v", err)
	}
	tickerList = filterLoadedTickers(tickerList, candleData)

	from, to, err := resolveDateRange(*dateFrom, *dateTo, candleData)
	if err != nil {
		logx.Fatalf("даты: %v", err)
	}

	settings := eval.RunSettings{
		Tickers:            tickerList,
		StrategyID:         *strategyID,
		StopMode:           *stopMode,
		ClassCode:          u.ClassCode,
		CandleTimeframe:    u.CandleTimeframe,
		Deposit:            *deposit,
		StepPriceValue:     *stepPrice,
		CommissionPerLot:     u.CommissionPerLot(*commission),
		MinTrades:          1,
		Session:            optimizer.DefaultSession(),
	}
	evaluator := eval.NewEvaluator(settings, space, candleData)
	params := eval.DefaultParamsFromSpace(space)

	ctx := context.Background()
	result := eval.RunBacktest(ctx, evaluator, from, to, params)

	fmt.Printf("strategy=%s period=%s → %s\n", result.StrategyID, from.Format("2006-01-02"), to.Format("2006-01-02"))
	fmt.Printf("trades=%d total_pnl=%.2f sharpe=%.4f max_dd=%.2f win_rate=%.2f\n",
		result.NumTrades, result.Metrics.TotalPnL, result.Metrics.Sharpe,
		result.Metrics.MaxDrawdown, result.Metrics.WinRate)
}

func chartsCmd(args []string) {
	fs := flag.NewFlagSet("charts", flag.ExitOnError)
	experiment := fs.String("experiment", "", "имя эксперимента: momentum_breakout или exp-momentum_breakout")
	resultsDir := fs.String("results-dir", "results", "директория с результатами optimizer")
	historyDir := fs.String("history-dir", "data/history", "директория CSV-истории")
	commission := fs.Float64("commission-per-lot", -1, "комиссия round-trip за акцию/контракт, руб (<=0: из tickers-config или class_code)")
	_ = fs.Parse(args)

	if *experiment == "" {
		logx.Fatal("укажите -experiment (например: momentum_breakout)")
	}

	ctx := context.Background()
	result, err := charts.RunCharts(ctx, charts.ChartsOptions{
		Experiment:     *experiment,
		ResultsDir:     *resultsDir,
		HistoryDir:     *historyDir,
		CommissionPerLot: *commission,
	})
	if err != nil {
		logx.Fatalf("charts: %v", err)
	}

	fmt.Printf("charts: experiment=%s dir=%s files=%d\n", result.Experiment, result.ChartsDir, len(result.Files))
	if result.ExportDir != "" {
		fmt.Printf("export: dir=%s (data-summary.json, data-trades.json, prompt-*.md)\n", result.ExportDir)
	}
	for _, f := range result.Files {
		fmt.Printf("  %s (%d trades, PnL %.0f руб.)\n", f.Path, f.Trades, f.PnL)
	}
}

func syncHistoryCmd(args []string) {
	fs := flag.NewFlagSet("sync-history", flag.ExitOnError)
	tickersConfigPath := fs.String("tickers-config", defaultTickersConfig, "YAML со списком инструментов")
	tickers := fs.String("tickers", "", "override тикеров через запятую")
	outputDir := fs.String("output-dir", "data/history", "директория для CSV")
	initialYears := fs.Int("initial-years", 0, "глубина первичной загрузки (0 = из tickers-config)")
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

	u, err := data.LoadTickersConfig(*tickersConfigPath)
	if err != nil {
		logx.Fatalf("tickers-config: %v", err)
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
		Fetch: data.FetchConfig{
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
	tickersConfigPath := fs.String("tickers-config", defaultTickersConfig, "YAML со списком инструментов")
	tickers := fs.String("tickers", "", "override тикеров")
	dateFrom := fs.String("date-from", "", "начало YYYY-MM-DD (default: initial_history_years назад)")
	dateTo := fs.String("date-to", "", "конец YYYY-MM-DD (default: сегодня)")
	outputDir := fs.String("output-dir", "data/history", "директория для CSV")
	_ = fs.Parse(args)

	token := os.Getenv("BCS_REFRESH_TOKEN")
	if token == "" {
		logx.Fatal("задайте BCS_REFRESH_TOKEN")
	}

	u, err := data.LoadTickersConfig(*tickersConfigPath)
	if err != nil {
		logx.Fatalf("tickers-config: %v", err)
	}
	tickerList := u.ResolveTickers(*tickers)

	now := time.Now()
	from := now.AddDate(-u.InitialHistoryYears, 0, 0)
	to := now
	if *dateFrom != "" {
		from, err = report.ParseDate(*dateFrom)
		if err != nil {
			logx.Fatalf("date-from: %v", err)
		}
	}
	if *dateTo != "" {
		to, err = report.ParseDate(*dateTo)
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
	if err := optimizer.FetchHistory(ctx, client, tickerList, u.ClassCode, u.CandleTimeframe, *outputDir, from, to, data.DefaultFetchConfig()); err != nil {
		logx.Fatalf("fetch-history: %v", err)
	}
}

// filterLoadedTickers возвращает только те тикеры, по которым действительно загружены свечи.
// Важно: порядок сохраняется как в requested, чтобы последующие шаги работали предсказуемо.
func filterLoadedTickers(requested []string, data map[string][]models.Candle) []string {
	out := make([]string, 0, len(data))
	for _, ticker := range requested {
		// Пропускаем тикеры без данных, чтобы не гонять их в бэктест/оптимизацию.
		if _, ok := data[ticker]; ok {
			out = append(out, ticker)
		}
	}
	return out
}

// resolveDateRange определяет рабочий диапазон дат для оптимизации/бэктеста.
// Приоритет: явные значения из флагов CLI, иначе границы берутся из доступного CSV.
func resolveDateRange(fromStr, toStr string, candleData map[string][]models.Candle) (time.Time, time.Time, error) {
	// Если обе даты заданы явно, используем их без обращения к данным.
	if fromStr != "" && toStr != "" {
		from, err := report.ParseDate(fromStr)
		if err != nil {
			return time.Time{}, time.Time{}, err
		}
		to, err := report.ParseDate(toStr)
		if err != nil {
			return time.Time{}, time.Time{}, err
		}
		return from, to, nil
	}

	// Базовый диапазон берём из фактически загруженных свечей.
	csvFrom, csvTo, ok := data.CandleDataRange(candleData)
	if !ok {
		return time.Time{}, time.Time{}, fmt.Errorf("нет данных в CSV")
	}

	from := csvFrom
	to := csvTo
	// Частично заданные флаги переопределяют соответствующую границу CSV-диапазона.
	if fromStr != "" {
		var err error
		from, err = report.ParseDate(fromStr)
		if err != nil {
			return time.Time{}, time.Time{}, err
		}
	}
	if toStr != "" {
		var err error
		to, err = report.ParseDate(toStr)
		if err != nil {
			return time.Time{}, time.Time{}, err
		}
	}
	return from, to, nil
}
