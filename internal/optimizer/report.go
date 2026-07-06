package optimizer

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"bcs-trading-bot/internal/config"
	"bcs-trading-bot/internal/strategy"
	"bcs-trading-bot/pkg/logx"

	"gopkg.in/yaml.v3"
)

// WindowResult — метрики одного окна.
type WindowResult struct {
	Train Metrics `json:"train"`
	Test  Metrics `json:"test"`
}

// TrialResult — результат одного trial.
type TrialResult struct {
	Index      int            `json:"index"`
	Params     ParameterSet   `json:"params"`
	TrainScore float64        `json:"train_score"`
	TestScore  float64        `json:"test_score"`
	Windows    []WindowResult `json:"windows"`
}

// RunResult — полный результат оптимизации.
type RunResult struct {
	Timestamp  time.Time     `json:"timestamp"`
	Trials     []TrialResult `json:"trials"`
	BestByTest TrialResult   `json:"best_by_test"`
}

<<<<<<< Updated upstream
// RunOptimization выполняет walk-forward random search.
func RunOptimization(ctx context.Context, evaluator *Evaluator, space *SearchSpace, windows []Window, trials, minTrades int, seed int64) *RunResult {
	searcher := NewRandomSearcher(space, trials, seed)
	var results []TrialResult
	interval := optimizerProgressInterval(trials)
	started := time.Now()
	bestTestSoFar := math.Inf(-1)

	logx.Info("optimizer: старт %d trials, %d walk-forward окон", trials, len(windows))

	for i := 0; i < trials; i++ {
		if ctx.Err() != nil {
			logx.Warn("optimizer: прервано на trial %d/%d", i, trials)
			break
		}
		params := searcher.Suggest()

		var trainScores, testScores []float64
		var windowResults []WindowResult

		for _, w := range windows {
			trainM := evaluator.EvaluatePeriod(ctx, params, w.TrainStart, w.TrainEnd)
			testM := evaluator.EvaluatePeriod(ctx, params, w.TestStart, w.TestEnd)
			trainScores = append(trainScores, Score(trainM, minTrades))
			testScores = append(testScores, Score(testM, minTrades))
			windowResults = append(windowResults, WindowResult{Train: trainM, Test: testM})
		}

		trainScore := MeanFloat(trainScores)
		testScore := MedianFloat(testScores)
		searcher.Report(params, trainScore)

		if testScore > bestTestSoFar {
			bestTestSoFar = testScore
		}

		results = append(results, TrialResult{
			Index:      i,
			Params:     params,
			TrainScore: trainScore,
			TestScore:  testScore,
			Windows:    windowResults,
		})

		done := i + 1
		if done == trials || done%interval == 0 {
			logOptimizerProgress(done, trials, bestTestSoFar, started)
		}
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].TestScore > results[j].TestScore
	})

	best := TrialResult{}
	if len(results) > 0 {
		best = results[0]
	}

	elapsed := time.Since(started).Round(time.Second)
	logx.Info("optimizer: готово %d/%d trials за %s, best test_score=%s",
		len(results), trials, elapsed, formatOptimizerScore(best.TestScore))
	if len(results) > 0 {
		bestPnL := totalTestPnL(best)
		switch {
		case math.IsInf(best.TestScore, -1):
			logx.Warn("optimizer: ни один trial не набрал min-trades на OOS — уменьшите -min-trades или сузьте search-space (breakoutThreshold > 0.05 почти не даёт сделок)")
		case best.TestScore < 0:
			logx.Warn("optimizer: OOS убыточен (score=%s, test PnL=%.0f руб.) — см. JSON",
				formatOptimizerScore(best.TestScore), bestPnL)
		case bestPnL <= 0:
			// TestScore — медиана Calmar по окнам: у части окон может быть неплохой
			// Calmar (мало сделок, маленькая просадка), а сумма PnL по всем окнам
			// всё равно в минусе. Положительный score НЕ означает, что конфигурация
			// заработала деньги за весь период — явно предупреждаем, иначе можно
			// принять "score > 0, предупреждения нет" за признак прибыльности.
			logx.Warn("optimizer: best test_score=%s положителен (медиана Calmar по окнам), но суммарный OOS PnL по всем окнам = %.0f руб. — конфигурация НЕ прибыльна в деньгах, см. JSON по окнам",
				formatOptimizerScore(best.TestScore), bestPnL)
		}
	}

	return &RunResult{
		Timestamp:  time.Now(),
		Trials:     results,
		BestByTest: best,
	}
}

=======
>>>>>>> Stashed changes
func optimizerProgressInterval(total int) int {
	switch {
	case total <= 5:
		return 1
	case total <= 25:
		return 5
	default:
		interval := total / 20
		if interval < 10 {
			return 10
		}
		return interval
	}
}

func logOptimizerProgress(done, total int, bestTest float64, started time.Time) {
	elapsed := time.Since(started)
	pct := done * 100 / total
	msg := fmt.Sprintf("optimizer: %d/%d (%d%%) best_test=%s elapsed=%s",
		done, total, pct, formatOptimizerScore(bestTest), elapsed.Round(time.Second))
	if done > 0 && done < total {
		eta := time.Duration(int64(elapsed) * int64(total-done) / int64(done))
		msg += fmt.Sprintf(" eta=%s", eta.Round(time.Second))
	}
	logx.Info("%s", msg)
}

func formatOptimizerScore(s float64) string {
	switch {
	case math.IsInf(s, -1):
		return "-Inf"
	case math.IsInf(s, 1):
		return "+Inf"
	case math.IsNaN(s):
		return "NaN"
	case math.Abs(s) >= 100:
		return fmt.Sprintf("%.0f", s)
	default:
		return fmt.Sprintf("%.4f", s)
	}
}

// WriteResults сохраняет JSON-отчёт и best-config YAML.
func WriteResults(outputDir string, result *RunResult, settings RunSettings, space *SearchSpace) (jsonPath, yamlPath string, err error) {
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return "", "", err
	}

	ts := result.Timestamp.Format("20060102-150405")
	jsonPath = filepath.Join(outputDir, fmt.Sprintf("optimizer-run-%s.json", ts))
	yamlPath = filepath.Join(outputDir, fmt.Sprintf("best-config-%s.yaml", ts))

	data, err := marshalRunResultJSON(result)
	if err != nil {
		return "", "", err
	}
	if err := os.WriteFile(jsonPath, data, 0o644); err != nil {
		return "", "", err
	}

	yamlData, err := yaml.Marshal(buildBestConfig(result.BestByTest.Params, settings, space, result.BestByTest))
	if err != nil {
		return "", "", err
	}
	if err := os.WriteFile(yamlPath, yamlData, 0o644); err != nil {
		return "", "", err
	}

	return jsonPath, yamlPath, nil
}

// PrintTopN выводит top-N конфигураций в stdout.
func PrintTopN(result *RunResult, n int) {
	if n > len(result.Trials) {
		n = len(result.Trials)
	}
	fmt.Println("=== Top configurations by OOS test score ===")
	for i := 0; i < n; i++ {
		t := result.Trials[i]
		fmt.Printf("\n#%d test_score=%s train_score=%s trades(test)=%d test_pnl=%.0f руб\n",
			i+1, formatOptimizerScore(t.TestScore), formatOptimizerScore(t.TrainScore), totalTestTrades(t), totalTestPnL(t))
		for _, key := range sortedParamKeys(t.Params) {
			fmt.Printf("  %s: %v\n", key, formatParam(t.Params[key]))
		}
	}
}

func totalTestPnL(t TrialResult) float64 {
	var sum float64
	for _, w := range t.Windows {
		sum += w.Test.TotalPnL
	}
	return sum
}

func totalTestTrades(t TrialResult) int {
	total := 0
	for _, w := range t.Windows {
		total += w.Test.NumTrades
	}
	return total
}

func sortedParamKeys(p ParameterSet) []string {
	keys := make([]string, 0, len(p))
	for k := range p {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func formatParam(v float64) string {
	if v == float64(int(v)) {
		return fmt.Sprintf("%d", int(v))
	}
	return fmt.Sprintf("%.4f", v)
}

type bestConfigYAML struct {
	TradingMode     string              `yaml:"trading_mode"`
	Tickers         []string            `yaml:"tickers"`
	ClassCode       string              `yaml:"class_code"`
	CandleTimeframe string              `yaml:"candle_timeframe"`
	Session         sessionYAML         `yaml:"session"`
	Strategy        strategyYAML        `yaml:"strategy"`
	Risk            riskYAML            `yaml:"risk"`
	Virtual         map[string]float64  `yaml:"virtual"`
	OptimizerNote   string              `yaml:"optimizer_note,omitempty"`
}

type sessionYAML struct {
	Timezone          string `yaml:"timezone"`
	EODCloseTime      string `yaml:"eod_close_time"`
	SessionOpenTime   string `yaml:"session_open_time"`
	EntryDelayMinutes int    `yaml:"entry_delay_minutes,omitempty"`
}

type strategyYAML struct {
	Type                     string  `yaml:"type"`
	Lookback                 int     `yaml:"lookback,omitempty"`
	StopMode                 string  `yaml:"stop_mode"`
	ATRPeriod                int     `yaml:"atr_period,omitempty"`
	ATRMultiplier            float64 `yaml:"atr_multiplier,omitempty"`
	RewardRatio              float64 `yaml:"reward_ratio,omitempty"`
	VolumeFilter             bool    `yaml:"volume_filter,omitempty"`
	VolumeMinRatio           float64 `yaml:"volume_min_ratio,omitempty"`
	BreakoutThreshold        float64 `yaml:"breakout_threshold,omitempty"`
	MaxTradesPerTickerPerDay int     `yaml:"max_trades_per_ticker_per_day,omitempty"`
	TrailActivationR         float64 `yaml:"trail_activation_r,omitempty"`
	TrailDiscreteStepR       float64 `yaml:"trail_discrete_step_r,omitempty"`
	TrailStageMax            int     `yaml:"trail_stage_max,omitempty"`
	LongOnly                 bool    `yaml:"long_only,omitempty"`
	TrendSMAPeriod           int     `yaml:"trend_sma_period,omitempty"`
	StrategyEntryDelayMinutes int    `yaml:"strategy_entry_delay_minutes,omitempty"`
	ORBMinutes               int     `yaml:"orb_minutes,omitempty"`
	FadeThreshold            float64 `yaml:"fade_threshold,omitempty"`
}

type riskYAML struct {
	Deposit             float64 `yaml:"deposit"`
	MaxDailyLossPercent float64 `yaml:"max_daily_loss_percent"`
	RiskPerTradePercent float64 `yaml:"risk_per_trade_percent"`
}

func buildBestConfig(params ParameterSet, settings RunSettings, space *SearchSpace, best TrialResult) bestConfigYAML {
	riskPct := space.FixedValue("riskPerTradePercent", 0.5)
	dailyLossPct := space.FixedValue("dailyLossLimitPercent", 2.0)

	p := make(strategy.Params, len(params))
	for k, v := range params {
		p[k] = v
	}
	ctx := strategy.BuildContext{
		StopMode: settings.StopMode,
		Session: strategy.SessionTimes{
			Timezone:          settings.Session.Timezone,
			SessionOpenTime:   settings.Session.SessionOpenTime,
			EntryDelayMinutes: settings.Session.EntryDelayMinutes,
		},
	}
	stratID := strategy.ResolveType(settings.StrategyID)
	if space.Strategy != "" {
		stratID = strategy.ResolveType(space.Strategy)
	}
	stratCfg := config.StrategyConfigFromOptimizer(stratID, p, ctx.StopMode, settings.Session)

	return bestConfigYAML{
		TradingMode:     "virtual",
		Tickers:         settings.Tickers,
		ClassCode:       settings.ClassCode,
		CandleTimeframe: settings.CandleTimeframe,
		Session: sessionYAML{
			Timezone:          settings.Session.Timezone,
			EODCloseTime:      settings.Session.EODCloseTime,
			SessionOpenTime:   settings.Session.SessionOpenTime,
			EntryDelayMinutes: settings.Session.EntryDelayMinutes,
		},
		Strategy: strategyYAMLFromConfig(stratCfg, params),
		Risk: riskYAML{
			Deposit:             settings.Deposit,
			MaxDailyLossPercent: dailyLossPct,
			RiskPerTradePercent: riskPct,
		},
		Virtual: map[string]float64{
			"balance": settings.Deposit,
		},
		OptimizerNote: fmt.Sprintf(
			"generated by optimizer; strategy=%s; OOS test_score=%s; apply manually — no auto-deploy",
			stratID, formatOptimizerScore(best.TestScore),
		),
	}
}

func strategyYAMLFromConfig(cfg config.StrategyConfig, params ParameterSet) strategyYAML {
	y := strategyYAML{
		Type:                      cfg.TypeOrDefault(),
		Lookback:                  cfg.Lookback,
		StopMode:                  cfg.StopMode,
		ATRPeriod:                 cfg.ATRPeriod,
		ATRMultiplier:             cfg.ATRMultiplier,
		RewardRatio:               cfg.RewardRatio,
		VolumeFilter:              cfg.VolumeFilterEnabled(),
		VolumeMinRatio:            cfg.VolumeMinRatio,
		BreakoutThreshold:         cfg.BreakoutThreshold,
		MaxTradesPerTickerPerDay:  cfg.MaxTradesPerTickerPerDay,
		LongOnly:                  cfg.LongOnlyEnabled(),
		TrendSMAPeriod:            cfg.TrendSMAPeriod,
		StrategyEntryDelayMinutes: cfg.StrategyEntryDelayMinutes,
		ORBMinutes:                cfg.ORBMinutes,
		FadeThreshold:             cfg.FadeThreshold,
		TrailActivationR:          params.FloatParam("trailActivationR"),
		TrailDiscreteStepR:        params.FloatParam("trailDiscreteStepR"),
		TrailStageMax:             params.IntParam("trailStageMax"),
	}
	if y.ATRPeriod == 0 {
		y.ATRPeriod = 14
	}
	if y.VolumeMinRatio == 0 {
		y.VolumeMinRatio = params.FloatParam("volumeFilterMultiplier")
	}
	if y.RewardRatio == 0 {
		y.RewardRatio = params.FloatParam("rewardRatio")
	}
	return y
}

// ParseDate парсит дату YYYY-MM-DD.
func ParseDate(s string) (time.Time, error) {
	return time.Parse("2006-01-02", strings.TrimSpace(s))
}

type jsonSafeRunResult struct {
	Timestamp  time.Time             `json:"timestamp"`
	Trials     []jsonSafeTrialResult `json:"trials"`
	BestByTest jsonSafeTrialResult   `json:"best_by_test"`
}

type jsonSafeTrialResult struct {
	Index      int            `json:"index"`
	Params     ParameterSet   `json:"params"`
	TrainScore *float64       `json:"train_score"`
	TestScore  *float64       `json:"test_score"`
	Windows    []WindowResult `json:"windows"`
}

func marshalRunResultJSON(result *RunResult) ([]byte, error) {
	safe := jsonSafeRunResult{
		Timestamp:  result.Timestamp,
		Trials:     make([]jsonSafeTrialResult, len(result.Trials)),
		BestByTest: toJSONSafeTrial(result.BestByTest),
	}
	for i, t := range result.Trials {
		safe.Trials[i] = toJSONSafeTrial(t)
	}
	return json.MarshalIndent(safe, "", "  ")
}

func toJSONSafeTrial(t TrialResult) jsonSafeTrialResult {
	return jsonSafeTrialResult{
		Index:      t.Index,
		Params:     t.Params,
		TrainScore: floatPtrForJSON(t.TrainScore),
		TestScore:  floatPtrForJSON(t.TestScore),
		Windows:    t.Windows,
	}
}

func floatPtrForJSON(f float64) *float64 {
	if math.IsInf(f, 0) || math.IsNaN(f) {
		return nil
	}
	out := f
	return &out
}
