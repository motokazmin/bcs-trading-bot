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
	Metrics Metrics `json:"metrics"`
}

// TrialResult — результат одного trial.
// Trial в optimizer — это одна попытка с конкретным набором параметров стратегии:
// 1) берём один sample из search space,
// 2) прогоняем его через все walk-forward окна,
// 3) агрегируем итоговый score и метрики по окнам.
type TrialResult struct {
	Index   int            `json:"index"`
	Params  ParameterSet   `json:"params"`
	Score   float64        `json:"score"`
	Windows []WindowResult `json:"windows"`
}

// RunResult — полный результат оптимизации.
type RunResult struct {
	Timestamp time.Time     `json:"timestamp"`
	Trials    []TrialResult `json:"trials"`
	Best      TrialResult   `json:"best"`
}

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

func logOptimizerProgress(done, total int, bestScore float64, started time.Time) {
	elapsed := time.Since(started)
	pct := done * 100 / total
	msg := fmt.Sprintf("optimizer: %d/%d (%d%%) best_score=%s elapsed=%s",
		done, total, pct, formatOptimizerScore(bestScore), elapsed.Round(time.Second))
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

	yamlData, err := yaml.Marshal(buildBestConfig(result.Best.Params, settings, space, result.Best))
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
	fmt.Println("=== Top configurations by walk-forward score ===")
	for i := 0; i < n; i++ {
		t := result.Trials[i]
		fmt.Printf("\n#%d score=%s trades=%d pnl=%.0f руб\n",
			i+1, formatOptimizerScore(t.Score), totalWindowTrades(t), totalWindowPnL(t))
		for _, key := range sortedParamKeys(t.Params) {
			fmt.Printf("  %s: %v\n", key, formatParam(t.Params[key]))
		}
	}
}

func totalWindowPnL(t TrialResult) float64 {
	var sum float64
	for _, w := range t.Windows {
		sum += w.Metrics.TotalPnL
	}
	return sum
}

func totalWindowTrades(t TrialResult) int {
	total := 0
	for _, w := range t.Windows {
		total += w.Metrics.NumTrades
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
	TradingMode     string             `yaml:"trading_mode"`
	Tickers         []string           `yaml:"tickers"`
	ClassCode       string             `yaml:"class_code"`
	CandleTimeframe string             `yaml:"candle_timeframe"`
	Costs           costsYAML          `yaml:"costs"`
	Session         sessionYAML        `yaml:"session"`
	Strategy        strategyYAML       `yaml:"strategy"`
	Risk            riskYAML           `yaml:"risk"`
	Virtual         map[string]float64 `yaml:"virtual"`
	OptimizerNote   string             `yaml:"optimizer_note,omitempty"`
}

type costsYAML struct {
	CommissionPerLot float64 `yaml:"commission_per_lot"`
}

type sessionYAML struct {
	Timezone          string `yaml:"timezone"`
	EODCloseTime      string `yaml:"eod_close_time"`
	SessionOpenTime   string `yaml:"session_open_time"`
	EntryDelayMinutes int    `yaml:"entry_delay_minutes,omitempty"`
}

type strategyYAML struct {
	Type                      string  `yaml:"type"`
	Lookback                  int     `yaml:"lookback,omitempty"`
	StopMode                  string  `yaml:"stop_mode"`
	ATRPeriod                 int     `yaml:"atr_period,omitempty"`
	ATRMultiplier             float64 `yaml:"atr_multiplier,omitempty"`
	RewardRatio               float64 `yaml:"reward_ratio,omitempty"`
	VolumeFilter              bool    `yaml:"volume_filter,omitempty"`
	VolumeMinRatio            float64 `yaml:"volume_min_ratio,omitempty"`
	BreakoutThreshold         float64 `yaml:"breakout_threshold,omitempty"`
	MaxTradesPerTickerPerDay  int     `yaml:"max_trades_per_ticker_per_day,omitempty"`
	TrailActivationR          float64 `yaml:"trail_activation_r,omitempty"`
	TrailDiscreteStepR        float64 `yaml:"trail_discrete_step_r,omitempty"`
	TrailStageMax             int     `yaml:"trail_stage_max,omitempty"`
	LongOnly                  bool    `yaml:"long_only,omitempty"`
	TrendSMAPeriod            int     `yaml:"trend_sma_period,omitempty"`
	StrategyEntryDelayMinutes int     `yaml:"strategy_entry_delay_minutes,omitempty"`
	ORBMinutes                int     `yaml:"orb_minutes,omitempty"`
	FadeThreshold             float64 `yaml:"fade_threshold,omitempty"`
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
		Costs: costsYAML{
			CommissionPerLot: settings.CommissionPerLot,
		},
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
			"generated by optimizer; strategy=%s; walk-forward score=%s; apply manually — no auto-deploy",
			stratID, formatOptimizerScore(best.Score),
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
	if y.RewardRatio == 0 {
		y.RewardRatio = strategy.DefaultRewardRatio(cfg.TypeOrDefault())
	}
	return y
}

// ParseDate парсит дату YYYY-MM-DD.
func ParseDate(s string) (time.Time, error) {
	return time.Parse("2006-01-02", strings.TrimSpace(s))
}

type jsonSafeRunResult struct {
	Timestamp time.Time             `json:"timestamp"`
	Trials    []jsonSafeTrialResult `json:"trials"`
	Best      jsonSafeTrialResult   `json:"best"`
}

type jsonSafeTrialResult struct {
	Index   int            `json:"index"`
	Params  ParameterSet   `json:"params"`
	Score   *float64       `json:"score"`
	Windows []WindowResult `json:"windows"`
}

func marshalRunResultJSON(result *RunResult) ([]byte, error) {
	safe := jsonSafeRunResult{
		Timestamp: result.Timestamp,
		Trials:    make([]jsonSafeTrialResult, len(result.Trials)),
		Best:      toJSONSafeTrial(result.Best),
	}
	for i, t := range result.Trials {
		safe.Trials[i] = toJSONSafeTrial(t)
	}
	return json.MarshalIndent(safe, "", "  ")
}

func toJSONSafeTrial(t TrialResult) jsonSafeTrialResult {
	return jsonSafeTrialResult{
		Index:   t.Index,
		Params:  t.Params,
		Score:   floatPtrForJSON(t.Score),
		Windows: t.Windows,
	}
}

func floatPtrForJSON(f float64) *float64 {
	if math.IsInf(f, 0) || math.IsNaN(f) {
		return nil
	}
	out := f
	return &out
}
