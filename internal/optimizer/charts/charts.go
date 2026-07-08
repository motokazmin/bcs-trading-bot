package charts

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"bcs-trading-bot/internal/config"
	core "bcs-trading-bot/internal/optimizer/core"
	"bcs-trading-bot/internal/optimizer/data"
	evalpkg "bcs-trading-bot/internal/optimizer/eval"
	"bcs-trading-bot/pkg/logx"
	"bcs-trading-bot/pkg/models"
)

//go:embed templates/chart.html
var chartHTMLTemplate string

// chartWindowPadding — отступ вокруг первой/последней сделки на графике.
const chartWindowPadding = 5 * 24 * time.Hour

// ChartsOptions — параметры генерации графиков по эксперименту.
type ChartsOptions struct {
	Experiment         string
	ResultsDir         string
	ExperimentDir      string // если задан — используется напрямую (например output optimizer run)
	HistoryDir         string
	CommissionPerLot float64
}

// ChartsBatchResult — итог пакетной генерации графиков.
type ChartsBatchResult struct {
	Results []*ChartsResult
	Errors  []ChartsBatchError
}

// ChartsBatchError — ошибка по одному эксперименту (остальные могут быть успешны).
type ChartsBatchError struct {
	Experiment string
	Err        error
}

// ChartsResult — итог генерации графиков.
type ChartsResult struct {
	Experiment string
	ChartsDir  string
	ExportDir  string
	Files      []ChartFileInfo
}

// ChartFileInfo — один сгенерированный HTML-файл.
type ChartFileInfo struct {
	Ticker string
	Path   string
	Trades int
	PnL    float64
}

// RunCharts генерирует HTML-графики по тикерам для эксперимента.
func RunCharts(ctx context.Context, opts ChartsOptions) (*ChartsResult, error) {
	expDir, err := resolveChartsExperimentDir(opts)
	if err != nil {
		return nil, err
	}

	cfgPath, err := LoadLatestBestConfigPath(expDir)
	if err != nil {
		return nil, err
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return nil, fmt.Errorf("загрузка конфига: %w", err)
	}

	tickers := cfg.TickerSymbols()
	if len(tickers) == 0 {
		return nil, fmt.Errorf("в конфиге нет тикеров")
	}

	candleData, err := evalpkg.LoadCandleData(opts.HistoryDir, tickers)
	if err != nil {
		return nil, err
	}

	from, to, ok := data.CandleDataRange(candleData)
	if !ok {
		return nil, fmt.Errorf("нет свечей для графиков")
	}
	to = to.Add(time.Minute)

	params := parameterSetFromConfig(cfg)
	space := &core.SearchSpace{
		Strategy: cfg.Strategy.TypeOrDefault(),
		Fixed: map[string]float64{
			"riskPerTradePercent":   cfg.Risk.RiskPerTradePercent,
			"dailyLossLimitPercent": cfg.Risk.MaxDailyLossPercent,
		},
	}

	chartsDir := filepath.Join(expDir, "charts")
	if err := os.RemoveAll(chartsDir); err != nil {
		return nil, fmt.Errorf("очистка charts: %w", err)
	}
	if err := os.MkdirAll(chartsDir, 0o755); err != nil {
		return nil, err
	}

	expName := strings.TrimPrefix(filepath.Base(expDir), "exp-")
	commission := opts.CommissionPerLot
	if commission <= 0 {
		commission = cfg.CommissionPerLot()
	}
	logx.Info("charts: experiment=%s tickers=%d period=%s → %s",
		expName, len(tickers), from.Format("2006-01-02")+"→"+to.Format("2006-01-02"), chartsDir)

	result := &ChartsResult{
		Experiment: expName,
		ChartsDir:  chartsDir,
	}

	var allTrades []models.ClosedTrade
	portfolioSettings := evalpkg.RunSettings{
		Tickers:          tickers,
		StrategyID:       cfg.Strategy.TypeOrDefault(),
		StopMode:         cfg.Strategy.StopMode,
		ClassCode:        cfg.ClassCode,
		CandleTimeframe:  cfg.CandleTimeFrame,
		Deposit:          cfg.Risk.Deposit,
		StepPriceValue:   1.0,
		CommissionPerLot: commission,
		Session:          cfg.Session,
	}
	portfolioEvaluator := evalpkg.NewEvaluator(portfolioSettings, space, candleData)
	portfolio := portfolioEvaluator.EvaluatePeriodDetailed(ctx, params, from, to)
	allTrades = append(allTrades, portfolio.Trades...)
	sortTradesByClose(allTrades)

	for _, ticker := range tickers {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}

		candles, ok := candleData[ticker]
		if !ok || len(candles) == 0 {
			logx.Warn("charts: пропуск %s — нет истории", ticker)
			continue
		}

		tFrom, tTo, ok := candleRange(candles)
		if !ok {
			continue
		}
		tTo = tTo.Add(time.Minute)

		settings := evalpkg.RunSettings{
			Tickers:            []string{ticker},
			StrategyID:         cfg.Strategy.TypeOrDefault(),
			StopMode:           cfg.Strategy.StopMode,
			ClassCode:          cfg.ClassCode,
			CandleTimeframe:    cfg.CandleTimeFrame,
			Deposit:            cfg.Risk.Deposit,
			StepPriceValue:     1.0,
			CommissionPerLot: commission,
			Session:            cfg.Session,
		}
		evaluator := evalpkg.NewEvaluator(settings, space, candleData)

		period := evaluator.EvaluateTickerPeriod(ctx, params, ticker, tFrom, tTo)
		if period.Metrics.NumTrades == 0 {
			logx.Info("charts: пропуск %s — нет сделок", ticker)
			continue
		}

		html, err := BuildChartHTML(ChartMeta{
			Experiment: expName,
			Ticker:     ticker,
			Strategy:   cfg.Strategy.TypeOrDefault(),
			PeriodFrom: tFrom,
			PeriodTo:   tTo,
			Metrics:    period.Metrics,
		}, candles, period.Trades, commission)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", ticker, err)
		}

		outPath := filepath.Join(chartsDir, ticker+".html")
		if err := os.WriteFile(outPath, html, 0o644); err != nil {
			return nil, err
		}

		info := ChartFileInfo{
			Ticker: ticker,
			Path:   outPath,
			Trades: period.Metrics.NumTrades,
			PnL:    period.Metrics.TotalPnL,
		}
		result.Files = append(result.Files, info)
		logx.Info("charts: %s (%d trades, PnL %.0f руб.) → %s", ticker, info.Trades, info.PnL, outPath)
	}

	if len(result.Files) == 0 {
		return nil, fmt.Errorf("не создано ни одного графика")
	}

	meta := loadOptimizerRunMeta(expDir)
	if meta != nil {
		meta.BestConfig = filepath.Base(cfgPath)
	}
	exportResult, err := WriteAnalysisExport(expDir, expName, cfg, cfgPath, allTrades, commission, meta)
	if err != nil {
		return result, fmt.Errorf("export: %w", err)
	}
	result.ExportDir = exportResult.Dir

	return result, nil
}

// ListExperimentDirs возвращает имена экспериментов (без префикса exp-)
// в results-dir, для которых есть best-config-*.yaml.
func ListExperimentDirs(resultsDir string) ([]string, error) {
	entries, err := os.ReadDir(resultsDir)
	if err != nil {
		return nil, fmt.Errorf("чтение %s: %w", resultsDir, err)
	}

	var names []string
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "exp-") {
			continue
		}
		expDir := filepath.Join(resultsDir, entry.Name())
		if _, err := LoadLatestBestConfigPath(expDir); err != nil {
			continue
		}
		names = append(names, strings.TrimPrefix(entry.Name(), "exp-"))
	}
	sort.Strings(names)
	return names, nil
}

// RunChartsAll генерирует графики для всех экспериментов в results-dir.
func RunChartsAll(ctx context.Context, opts ChartsOptions) (*ChartsBatchResult, error) {
	names, err := ListExperimentDirs(opts.ResultsDir)
	if err != nil {
		return nil, err
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("в %s нет экспериментов с best-config", opts.ResultsDir)
	}

	batch := &ChartsBatchResult{}
	for _, name := range names {
		if ctx.Err() != nil {
			return batch, ctx.Err()
		}
		runOpts := opts
		runOpts.Experiment = name
		result, err := RunCharts(ctx, runOpts)
		if err != nil {
			batch.Errors = append(batch.Errors, ChartsBatchError{Experiment: name, Err: err})
			logx.Warn("charts: %s: %v", name, err)
			continue
		}
		batch.Results = append(batch.Results, result)
	}
	if len(batch.Results) == 0 {
		return batch, fmt.Errorf("не создано ни одного графика")
	}
	return batch, nil
}

func resolveChartsExperimentDir(opts ChartsOptions) (string, error) {
	if opts.ExperimentDir != "" {
		st, err := os.Stat(opts.ExperimentDir)
		if err != nil {
			return "", fmt.Errorf("директория эксперимента: %w", err)
		}
		if !st.IsDir() {
			return "", fmt.Errorf("не директория: %s", opts.ExperimentDir)
		}
		return opts.ExperimentDir, nil
	}
	return ResolveExperimentDir(opts.ResultsDir, opts.Experiment)
}

// ResolveExperimentDir возвращает путь results/exp-{name}.
func ResolveExperimentDir(resultsDir, name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("experiment не задан")
	}
	name = strings.TrimPrefix(name, "exp-")
	expDir := filepath.Join(resultsDir, "exp-"+name)
	if st, err := os.Stat(expDir); err != nil || !st.IsDir() {
		return "", fmt.Errorf("эксперимент не найден: %s", expDir)
	}
	return expDir, nil
}

// LoadLatestBestConfigPath возвращает путь к последнему best-config в директории эксперимента.
func LoadLatestBestConfigPath(expDir string) (string, error) {
	matches, err := filepath.Glob(filepath.Join(expDir, "best-config-*.yaml"))
	if err != nil {
		return "", err
	}
	if len(matches) == 0 {
		return "", fmt.Errorf("best-config не найден в %s", expDir)
	}
	sort.Strings(matches)
	return matches[len(matches)-1], nil
}

// ChartMeta — заголовок графика.
type ChartMeta struct {
	Experiment string
	Ticker     string
	Strategy   string
	PeriodFrom time.Time
	PeriodTo   time.Time
	Metrics    core.Metrics
}

type chartPayload struct {
	Candles []chartCandle    `json:"candles"`
	Markers []chartMarker    `json:"markers"`
	Trades  []chartTradeSpan `json:"trades"`
}

type chartCandle struct {
	Time  int64   `json:"time"`
	Open  float64 `json:"open"`
	High  float64 `json:"high"`
	Low   float64 `json:"low"`
	Close float64 `json:"close"`
}

type chartTradeSpan struct {
	Index             int     `json:"index"`
	Direction         string  `json:"direction"`
	EntryTime         int64   `json:"entryTime"`
	ExitTime          int64   `json:"exitTime"`
	EntryPrice        float64 `json:"entryPrice"`
	ExitPrice         float64 `json:"exitPrice"`
	PnL               float64 `json:"pnl"`
	EntryLabel        string  `json:"entryLabel"`
	ExitLabel         string  `json:"exitLabel"`
	CloseReason       string  `json:"closeReason"`
	CloseReasonLabel  string  `json:"closeReasonLabel"`
}

type chartMarker struct {
	Time     int64  `json:"time"`
	Position string `json:"position"`
	Color    string `json:"color"`
	Shape    string `json:"shape"`
	Text     string `json:"text,omitempty"`
	Size     int    `json:"size,omitempty"`
}

type chartTemplateData struct {
	Title    string
	Subtitle string
	Stats    []chartStatItem
}

type chartStatItem struct {
	Label string
	Value string
	Class string
}

// BuildChartHTML строит интерактивный HTML с line chart и маркерами сделок.
func BuildChartHTML(meta ChartMeta, candles []models.Candle, trades []models.ClosedTrade, commission float64) ([]byte, error) {
	visible := candles
	if windowFrom, windowTo, ok := tradeChartWindow(trades, chartWindowPadding); ok {
		visible = data.FilterCandles(candles, windowFrom, windowTo.Add(5*time.Minute))
		meta.PeriodFrom = windowFrom
		meta.PeriodTo = windowTo
	}

	payload := chartPayload{
		Candles: candlesToChartCandles(visible),
		Markers: tradesToChartMarkers(trades, commission),
		Trades:  tradesToChartSpans(trades, commission),
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	tmpl, err := template.New("chart").Parse(chartHTMLTemplate)
	if err != nil {
		return nil, err
	}

	var body bytes.Buffer
	subtitle := fmt.Sprintf("%s · %s → %s · время МСК",
		meta.Strategy,
		formatChartDateMSK(meta.PeriodFrom),
		formatChartDateMSK(meta.PeriodTo),
	)
	if err := tmpl.Execute(&body, chartTemplateData{
		Title:    fmt.Sprintf("%s / %s", meta.Experiment, meta.Ticker),
		Subtitle: subtitle,
		Stats:    chartStats(meta.Metrics, trades, commission),
	}); err != nil {
		return nil, err
	}

	html := bytes.ReplaceAll(body.Bytes(), []byte("__CHART_DATA__"), payloadJSON)
	return html, nil
}

func chartStats(m core.Metrics, trades []models.ClosedTrade, commission float64) []chartStatItem {
	wins, losses := 0, 0
	for _, t := range trades {
		net := core.NetPnLFromGross(t.GrossPnL, t.Quantity, commission)
		if net > 0 {
			wins++
		} else if net < 0 {
			losses++
		}
	}

	pnlClass := "neutral"
	if m.TotalPnL > 0 {
		pnlClass = "positive"
	} else if m.TotalPnL < 0 {
		pnlClass = "negative"
	}

	avgPnL := 0.0
	if m.NumTrades > 0 {
		avgPnL = m.TotalPnL / float64(m.NumTrades)
	}

	expectancyR := avgExpectancyR(trades, commission)
	expClass := "neutral"
	if expectancyR > 0 {
		expClass = "positive"
	} else if expectancyR < 0 {
		expClass = "negative"
	}

	return []chartStatItem{
		{Label: "Сделок", Value: fmt.Sprintf("%d", m.NumTrades), Class: "neutral"},
		{Label: "Expectancy (R)", Value: fmt.Sprintf("%+.2f R", expectancyR), Class: expClass},
		{Label: "PnL", Value: fmt.Sprintf("%.0f ₽", m.TotalPnL), Class: pnlClass},
		{Label: "Expectancy (₽)", Value: fmt.Sprintf("%+.0f ₽", avgPnL), Class: pnlClass},
		{Label: "Win rate", Value: fmt.Sprintf("%.0f%%", m.WinRate*100), Class: "neutral"},
		{Label: "В плюсе", Value: fmt.Sprintf("%d", wins), Class: "positive"},
		{Label: "В минусе", Value: fmt.Sprintf("%d", losses), Class: "negative"},
		{Label: "Max DD", Value: fmt.Sprintf("%.0f ₽", m.MaxDrawdown), Class: "negative"},
	}
}

func avgExpectancyR(trades []models.ClosedTrade, commission float64) float64 {
	if len(trades) == 0 {
		return 0
	}
	sum := 0.0
	for _, t := range trades {
		net := core.NetPnLFromGross(t.GrossPnL, t.Quantity, commission)
		step := t.StepPriceValue
		if step <= 0 {
			step = 1.0
		}
		riskAmt := t.RDistance * float64(t.Quantity) * step
		if riskAmt > 0 {
			sum += net / riskAmt
		} else {
			sum += t.PnLR
		}
	}
	return sum / float64(len(trades))
}

func candlesToChartCandles(candles []models.Candle) []chartCandle {
	out := make([]chartCandle, len(candles))
	for i, c := range candles {
		out[i] = chartCandle{
			Time:  c.Timestamp.Unix(),
			Open:  c.Open,
			High:  c.High,
			Low:   c.Low,
			Close: c.Close,
		}
	}
	return out
}

func tradesToChartSpans(trades []models.ClosedTrade, commission float64) []chartTradeSpan {
	sorted := sortTradesByOpen(trades)
	out := make([]chartTradeSpan, len(sorted))
	for i, t := range sorted {
		out[i] = chartTradeSpan{
			Index:            i + 1,
			Direction:        strings.ToUpper(t.Direction),
			EntryTime:        t.OpenedAt.Unix(),
			ExitTime:         t.ClosedAt.Unix(),
			EntryPrice:       t.EntryPrice,
			ExitPrice:        t.ExitPrice,
			PnL:              core.NetPnLFromGross(t.GrossPnL, t.Quantity, commission),
			EntryLabel:       formatChartTimeMSK(t.OpenedAt),
			ExitLabel:        formatChartTimeMSK(t.ClosedAt),
			CloseReason:      t.CloseReason,
			CloseReasonLabel: formatCloseReasonLabel(t.CloseReason),
		}
	}
	return out
}

func sortTradesByOpen(trades []models.ClosedTrade) []models.ClosedTrade {
	sorted := append([]models.ClosedTrade(nil), trades...)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].OpenedAt.Before(sorted[j].OpenedAt)
	})
	return sorted
}

func tradesToChartMarkers(trades []models.ClosedTrade, commission float64) []chartMarker {
	sorted := sortTradesByOpen(trades)
	markers := make([]chartMarker, 0, len(sorted)*2)
	for i, t := range sorted {
		n := i + 1
		isLong := strings.EqualFold(t.Direction, "BUY")
		entryPos := "belowBar"
		entryColor := "#26a69a"
		entryShape := "arrowUp"
		exitPos := "aboveBar"
		if !isLong {
			entryPos = "aboveBar"
			entryColor = "#ef5350"
			entryShape = "arrowDown"
			exitPos = "belowBar"
		}
		markers = append(markers, chartMarker{
			Time:     t.OpenedAt.Unix(),
			Position: entryPos,
			Color:    entryColor,
			Shape:    entryShape,
			Text:     fmt.Sprintf("%d ВХ", n),
			Size:     2,
		})

		net := core.NetPnLFromGross(t.GrossPnL, t.Quantity, commission)
		exitColor := "#e57373"
		if net > 0 {
			exitColor = "#66bb6a"
		}
		markers = append(markers, chartMarker{
			Time:     t.ClosedAt.Unix(),
			Position: exitPos,
			Color:    exitColor,
			Shape:    "square",
			Text:     fmt.Sprintf("%d ВЫХ %s", n, formatCloseReasonShort(t.CloseReason)),
			Size:     2,
		})
	}
	sort.Slice(markers, func(i, j int) bool { return markers[i].Time < markers[j].Time })
	return markers
}

func parameterSetFromConfig(cfg *config.Config) core.ParameterSet {
	s := cfg.Strategy
	p := core.ParameterSet{
		"lookback":                  float64(s.Lookback),
		"atrPeriod":                 float64(s.ATRPeriod),
		"atrMultiplier":             s.ATRMultiplier,
		"rewardRatio":               s.RewardRatio,
		"breakoutThreshold":         s.BreakoutThreshold,
		"volumeMinRatio":            s.VolumeMinRatio,
		"maxEntriesPerTickerPerDay": float64(s.MaxTradesPerTickerPerDay),
		"orbMinutes":                float64(s.ORBMinutes),
		"fadeThreshold":             s.FadeThreshold,
		"trendSMAPeriod":            float64(s.TrendSMAPeriod),
		"strategyEntryDelayMinutes": float64(s.StrategyEntryDelayMinutes),
		"trailActivationR":          s.TrailActivationR,
		"trailDiscreteStepR":        s.TrailDiscreteStepR,
		"trailStageMax":             float64(s.TrailStageMax),
	}
	if s.VolumeFilterEnabled() {
		p["volumeFilter"] = 1
		if p["volumeMinRatio"] == 0 {
			p["volumeMinRatio"] = 1.5
		}
	}
	if s.LongOnlyEnabled() {
		p["longOnly"] = 1
	}
	if s.RangeUseCap != nil && !*s.RangeUseCap {
		p["rangeUseCap"] = 0
	} else {
		p["rangeUseCap"] = 1
	}
	return p
}

func candleRange(candles []models.Candle) (from, to time.Time, ok bool) {
	if len(candles) == 0 {
		return time.Time{}, time.Time{}, false
	}
	return candles[0].Timestamp, candles[len(candles)-1].Timestamp, true
}

// tradeChartWindow возвращает диапазон от первой до последней сделки с padding.
func tradeChartWindow(trades []models.ClosedTrade, padding time.Duration) (from, to time.Time, ok bool) {
	if len(trades) == 0 {
		return time.Time{}, time.Time{}, false
	}
	from = trades[0].OpenedAt
	to = trades[0].ClosedAt
	for _, t := range trades[1:] {
		if t.OpenedAt.Before(from) {
			from = t.OpenedAt
		}
		if t.ClosedAt.After(to) {
			to = t.ClosedAt
		}
	}
	return from.Add(-padding), to.Add(padding), true
}

var chartMoscowLoc *time.Location

func chartMoscow() *time.Location {
	if chartMoscowLoc == nil {
		loc, err := time.LoadLocation("Europe/Moscow")
		if err != nil {
			loc = time.FixedZone("MSK", 3*3600)
		}
		chartMoscowLoc = loc
	}
	return chartMoscowLoc
}

func formatChartTimeMSK(t time.Time) string {
	return t.In(chartMoscow()).Format("2006-01-02 15:04") + " МСК"
}

func formatChartDateMSK(t time.Time) string {
	return t.In(chartMoscow()).Format("2006-01-02")
}

func formatCloseReasonLabel(reason string) string {
	switch reason {
	case models.CloseReasonEOD:
		return "EOD — конец дня"
	case models.CloseReasonStopLoss:
		return "Стоп-лосс"
	case models.CloseReasonTakeProfit:
		return "Тейк-профит"
	case models.CloseReasonSmoke:
		return "Smoke-тест"
	default:
		if reason == "" {
			return "—"
		}
		return reason
	}
}

func formatCloseReasonShort(reason string) string {
	switch reason {
	case models.CloseReasonEOD:
		return "EOD"
	case models.CloseReasonStopLoss:
		return "SL"
	case models.CloseReasonTakeProfit:
		return "TP"
	case models.CloseReasonSmoke:
		return "SMOKE"
	default:
		if reason == "" {
			return "?"
		}
		return reason
	}
}
