package config

import (
	"fmt"
	"os"
	"strings"

	"bcs-trading-bot/internal/costs"
	"bcs-trading-bot/internal/strategy"
	"bcs-trading-bot/internal/trailing"

	"gopkg.in/yaml.v3"
)

const (
	TradingModeVirtual = "virtual"
	TradingModeReal    = "real"

	defaultExperimentID = "default"

	defaultClassCode       = "TQBR"
	defaultCandleTimeFrame = "M5"
	defaultDeposit         = 100_000
	defaultLookback        = 20
	defaultMaxDailyLossPct = 2.0
	defaultRiskPerTradePct = 0.5
	defaultStepPriceValue  = 1.0
	defaultTimezone        = "Europe/Moscow"
	defaultEODCloseTime    = "23:40"
	defaultSessionOpenTime = "10:00"
	defaultStoragePath     = "data/trades.db"
	defaultATRPeriod       = 14
	defaultATRMultiplier   = 2.0
)

// Config описывает все настройки бота, кроме секретов (токен — только из env).
type Config struct {
	TradingMode     string             `yaml:"trading_mode"`
	Tickers         []TickerConfig     `yaml:"tickers"`
	ClassCode       string             `yaml:"class_code"`
	CandleTimeFrame string             `yaml:"candle_timeframe"`
	Costs           costs.Config       `yaml:"costs"`
	Portfolio       PortfolioConfig    `yaml:"portfolio"`
	Risk            RiskConfig         `yaml:"risk"`
	Strategy        StrategyConfig     `yaml:"strategy"`
	Virtual         VirtualConfig      `yaml:"virtual"`
	Session         SessionConfig      `yaml:"session"`
	Storage         StorageConfig      `yaml:"storage"`
	Experiments     []ExperimentConfig `yaml:"experiments"`
}

// PortfolioConfig — режим единого счёта для нескольких experiments.
type PortfolioConfig struct {
	// SharedAccount: один VirtualExecutor + один GlobalRisk на все experiments.
	SharedAccount bool `yaml:"shared_account"`
}

// ExperimentConfig — изолированный виртуальный счёт с собственными параметрами стратегии и риска.
type ExperimentConfig struct {
	ID                string         `yaml:"id"`
	Name              string         `yaml:"name"`
	Tickers           []TickerConfig `yaml:"tickers"`
	EntryDelayMinutes *int           `yaml:"entry_delay_minutes"`
	Strategy          StrategyConfig `yaml:"strategy"`
	Risk              RiskConfig     `yaml:"risk"`
	Virtual           VirtualConfig  `yaml:"virtual"`
}

// ResolvedExperiment — нормализованный эксперимент, готовый к запуску воркеров.
type ResolvedExperiment struct {
	ID                string
	Name              string
	Tickers           []TickerConfig
	EntryDelayMinutes *int
	Strategy          StrategyConfig
	Risk              RiskConfig
	Virtual           VirtualConfig
}

type StorageConfig struct {
	Enabled *bool  `yaml:"enabled"`
	Path    string `yaml:"path"`
}

// StorageEnabled возвращает true, если персистентность включена (по умолчанию — да).
func (c *Config) StorageEnabled() bool {
	if c.Storage.Enabled == nil {
		return true
	}
	return *c.Storage.Enabled
}

// TickerConfig описывает инструмент и его параметры для расчёта риска.
type TickerConfig struct {
	Symbol         string
	StepPriceValue float64 `yaml:"step_price_value"`
}

func (t *TickerConfig) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var s string
	if err := unmarshal(&s); err == nil {
		t.Symbol = strings.TrimSpace(strings.ToUpper(s))
		t.StepPriceValue = defaultStepPriceValue
		return nil
	}

	var raw struct {
		Ticker         string  `yaml:"ticker"`
		StepPriceValue float64 `yaml:"step_price_value"`
	}
	if err := unmarshal(&raw); err != nil {
		return err
	}
	t.Symbol = strings.TrimSpace(strings.ToUpper(raw.Ticker))
	t.StepPriceValue = raw.StepPriceValue
	return nil
}

type RiskConfig struct {
	Deposit             float64 `yaml:"deposit"`
	MaxDailyLoss        float64 `yaml:"max_daily_loss"`
	MaxDailyLossPercent float64 `yaml:"max_daily_loss_percent"`
	RiskPerTradePercent float64 `yaml:"risk_per_trade_percent"`
	MaxParallelTrades   int     `yaml:"max_parallel_trades"`
}

type StrategyConfig struct {
	Type                      string  `yaml:"type"`
	Lookback                  int     `yaml:"lookback"`
	StopMode                  string  `yaml:"stop_mode"`
	ATRPeriod                 int     `yaml:"atr_period"`
	ATRMultiplier             float64 `yaml:"atr_multiplier"`
	RewardRatio               float64 `yaml:"reward_ratio"`
	RangeUseCap               *bool   `yaml:"range_use_cap"`
	MaxTradesPerTickerPerDay  int     `yaml:"max_trades_per_ticker_per_day"`
	VolumeFilter              *bool   `yaml:"volume_filter"`
	VolumeMinRatio            float64 `yaml:"volume_min_ratio"`
	BreakoutThreshold         float64 `yaml:"breakout_threshold"`
	TrailActivationR          float64 `yaml:"trail_activation_r"`
	TrailDiscreteStepR        float64 `yaml:"trail_discrete_step_r"`
	TrailStageMax             int     `yaml:"trail_stage_max"`
	TrailBreakevenR           float64 `yaml:"trail_breakeven_r"`
	LongOnly                  *bool   `yaml:"long_only"`
	TrendSMAPeriod            int     `yaml:"trend_sma_period"`
	StrategyEntryDelayMinutes int     `yaml:"strategy_entry_delay_minutes"`
	ORBMinutes                int     `yaml:"orb_minutes"`
	FadeThreshold             float64 `yaml:"fade_threshold"`
	FadeWindowMinutes         int     `yaml:"fade_window_minutes"`
	FadeTradeEndMinutes       int     `yaml:"fade_trade_end_minutes"`
	RequireInsideRange        *bool   `yaml:"require_inside_range"`
	MinMinutesAboveVWAP       int     `yaml:"min_minutes_above_vwap"`
	CompressionPercentile     float64 `yaml:"compression_percentile"`
	ATRBars                   int     `yaml:"atr_bars"`
	EntryStartMinutes         int     `yaml:"entry_start_minutes"`
	EntryEndMinutes           int     `yaml:"entry_end_minutes"`
	RangeStartMinutes         int     `yaml:"range_start_minutes"`
	RangeEndMinutes           int     `yaml:"range_end_minutes"`
}

type VirtualConfig struct {
	Balance float64 `yaml:"balance"`
}

type SessionConfig struct {
	Timezone          string `yaml:"timezone"`
	EODCloseTime      string `yaml:"eod_close_time"`
	SessionOpenTime   string `yaml:"session_open_time"`
	EntryDelayMinutes int    `yaml:"entry_delay_minutes"`
}

// TickerSymbols возвращает список символов тикеров для логирования.
func (c *Config) TickerSymbols() []string {
	symbols := make([]string, len(c.Tickers))
	for i, t := range c.Tickers {
		symbols[i] = t.Symbol
	}
	return symbols
}

// HasExperiments возвращает true, если в конфиге задана секция experiments.
func (c *Config) HasExperiments() bool {
	return len(c.Experiments) > 0
}

// ResolvedExperiments возвращает список экспериментов (или один default из корневых полей).
func (c *Config) ResolvedExperiments() []ResolvedExperiment {
	if !c.HasExperiments() {
		return []ResolvedExperiment{{
			ID:       defaultExperimentID,
			Name:     defaultExperimentID,
			Strategy: c.Strategy,
			Risk:     c.Risk,
			Virtual:  c.Virtual,
		}}
	}

	out := make([]ResolvedExperiment, len(c.Experiments))
	for i, exp := range c.Experiments {
		out[i] = ResolvedExperiment{
			ID:                exp.ID,
			Name:              exp.Name,
			Tickers:           exp.Tickers,
			EntryDelayMinutes: exp.EntryDelayMinutes,
			Strategy:          exp.Strategy,
			Risk:              exp.Risk,
			Virtual:           exp.Virtual,
		}
	}
	return out
}

// TickersForExperiment возвращает список тикеров эксперимента (свой или корневой).
func (c *Config) TickersForExperiment(exp ResolvedExperiment) []TickerConfig {
	if len(exp.Tickers) == 0 {
		return c.Tickers
	}
	rootBySymbol := make(map[string]TickerConfig, len(c.Tickers))
	for _, t := range c.Tickers {
		rootBySymbol[t.Symbol] = t
	}
	out := make([]TickerConfig, len(exp.Tickers))
	for i, t := range exp.Tickers {
		if t.StepPriceValue <= 0 {
			if root, ok := rootBySymbol[t.Symbol]; ok {
				out[i] = root
				continue
			}
			t.StepPriceValue = defaultStepPriceValue
		}
		out[i] = t
	}
	return out
}

// SessionForExperiment возвращает сессию с учётом переопределений эксперимента.
func (c *Config) SessionForExperiment(exp ResolvedExperiment) SessionConfig {
	s := c.Session
	if exp.EntryDelayMinutes != nil {
		s.EntryDelayMinutes = *exp.EntryDelayMinutes
	}
	return s
}

// AllTickerSymbols возвращает объединение тикеров всех экспериментов (для WebSocket).
func (c *Config) AllTickerSymbols() []string {
	seen := make(map[string]struct{})
	var out []string
	for _, exp := range c.ResolvedExperiments() {
		for _, t := range c.TickersForExperiment(exp) {
			if _, ok := seen[t.Symbol]; ok {
				continue
			}
			seen[t.Symbol] = struct{}{}
			out = append(out, t.Symbol)
		}
	}
	return out
}

// PerTickerDeposit возвращает депозит, выделенный на один тикер (корневой конфиг).
func (c *Config) PerTickerDeposit() float64 {
	return c.Risk.Deposit / float64(len(c.Tickers))
}

// PerTickerMaxDailyLoss возвращает дневной лимит убытка на один тикер (корневой конфиг).
func (c *Config) PerTickerMaxDailyLoss() float64 {
	return c.Risk.MaxDailyLoss / float64(len(c.Tickers))
}

// PerTickerDepositForExperiment возвращает депозит на тикер внутри эксперимента.
func (e ResolvedExperiment) PerTickerDeposit(tickerCount int) float64 {
	if tickerCount <= 0 {
		return 0
	}
	return e.Risk.Deposit / float64(tickerCount)
}

// PerTickerMaxDailyLossForExperiment возвращает дневной лимит убытка на тикер внутри эксперимента.
func (e ResolvedExperiment) PerTickerMaxDailyLoss(tickerCount int) float64 {
	if tickerCount <= 0 {
		return 0
	}
	return e.Risk.MaxDailyLoss / float64(tickerCount)
}

// SharedAccountEnabled — единый virtual-счёт и GlobalRisk на все experiments.
func (c *Config) SharedAccountEnabled() bool {
	return c.Portfolio.SharedAccount
}

// SharedRisk возвращает корневой risk для shared account (fallback — первый experiment).
func (c *Config) SharedRisk() RiskConfig {
	if c.Risk.Deposit > 0 {
		return c.Risk
	}
	exps := c.ResolvedExperiments()
	if len(exps) > 0 {
		return exps[0].Risk
	}
	return c.Risk
}

// SharedVirtualBalance — баланс единого VirtualExecutor.
func (c *Config) SharedVirtualBalance() float64 {
	if c.Virtual.Balance > 0 {
		return c.Virtual.Balance
	}
	return c.SharedRisk().Deposit
}

// TypeOrDefault возвращает type стратегии (default momentum_breakout).
func (s StrategyConfig) TypeOrDefault() string {
	t := strings.TrimSpace(s.Type)
	if t == "" {
		return strategy.DefaultType()
	}
	return t
}

// EffectiveRewardRatio — фактический R:R (с дефолтом по типу стратегии).
func (s StrategyConfig) EffectiveRewardRatio() float64 {
	if s.RewardRatio > 0 {
		return s.RewardRatio
	}
	return strategy.DefaultRewardRatio(s.TypeOrDefault())
}

func (s StrategyConfig) LongOnlyEnabled() bool {
	if s.LongOnly == nil {
		return false
	}
	return *s.LongOnly
}

// StrategyConfigFromMap собирает StrategyConfig из полей optimizer (snake_case keys).
func StrategyConfigFromMap(fields map[string]interface{}, stopMode string) StrategyConfig {
	cfg := StrategyConfig{StopMode: stopMode}
	if v, ok := fields["type"].(string); ok {
		cfg.Type = v
	}
	if v, ok := fields["lookback"].(int); ok {
		cfg.Lookback = v
	}
	if v, ok := fields["stop_mode"].(string); ok && v != "" {
		cfg.StopMode = v
	}
	if v, ok := fields["atr_period"].(int); ok {
		cfg.ATRPeriod = v
	}
	if v, ok := fields["atr_multiplier"].(float64); ok {
		cfg.ATRMultiplier = v
	}
	if v, ok := fields["reward_ratio"].(float64); ok {
		cfg.RewardRatio = v
	}
	if v, ok := fields["breakout_threshold"].(float64); ok {
		cfg.BreakoutThreshold = v
	}
	if v, ok := fields["fade_threshold"].(float64); ok {
		cfg.FadeThreshold = v
	}
	if v, ok := fields["orb_minutes"].(int); ok {
		cfg.ORBMinutes = v
	}
	if v, ok := fields["trend_sma_period"].(int); ok {
		cfg.TrendSMAPeriod = v
	}
	if v, ok := fields["strategy_entry_delay_minutes"].(int); ok {
		cfg.StrategyEntryDelayMinutes = v
	}
	if v, ok := fields["max_trades_per_ticker_per_day"].(int); ok {
		cfg.MaxTradesPerTickerPerDay = v
	}
	if v, ok := fields["volume_filter"].(bool); ok {
		b := v
		cfg.VolumeFilter = &b
	}
	if v, ok := fields["volume_min_ratio"].(float64); ok {
		cfg.VolumeMinRatio = v
	}
	if v, ok := fields["long_only"].(bool); ok {
		b := v
		cfg.LongOnly = &b
	}
	if v, ok := fields["fade_window_minutes"].(int); ok {
		cfg.FadeWindowMinutes = v
	}
	if v, ok := fields["fade_trade_end_minutes"].(int); ok {
		cfg.FadeTradeEndMinutes = v
	}
	if v, ok := fields["require_inside_range"].(bool); ok {
		b := v
		cfg.RequireInsideRange = &b
	}
	if v, ok := fields["min_minutes_above_vwap"].(int); ok {
		cfg.MinMinutesAboveVWAP = v
	}
	if v, ok := fields["compression_percentile"].(float64); ok {
		cfg.CompressionPercentile = v
	}
	if v, ok := fields["atr_bars"].(int); ok {
		cfg.ATRBars = v
	}
	if v, ok := fields["entry_start_minutes"].(int); ok {
		cfg.EntryStartMinutes = v
	}
	if v, ok := fields["entry_end_minutes"].(int); ok {
		cfg.EntryEndMinutes = v
	}
	if v, ok := fields["range_start_minutes"].(int); ok {
		cfg.RangeStartMinutes = v
	}
	if v, ok := fields["range_end_minutes"].(int); ok {
		cfg.RangeEndMinutes = v
	}
	if v, ok := fields["trail_activation_r"].(float64); ok {
		cfg.TrailActivationR = v
	}
	if v, ok := fields["trail_discrete_step_r"].(float64); ok {
		cfg.TrailDiscreteStepR = v
	}
	if v, ok := fields["trail_stage_max"].(int); ok {
		cfg.TrailStageMax = v
	}
	if v, ok := fields["trail_breakeven_r"].(float64); ok {
		cfg.TrailBreakevenR = v
	}
	return cfg
}

// StrategyOptions конвертирует конфиг стратегии в параметры MomentumBreakout (legacy).
func (s StrategyConfig) StrategyOptions() strategy.Options {
	rangeUseCap := true
	if s.RangeUseCap != nil {
		rangeUseCap = *s.RangeUseCap
	}

	opts := strategy.Options{
		Lookback:          s.Lookback,
		StopMode:          strings.ToLower(strings.TrimSpace(s.StopMode)),
		ATRPeriod:         s.ATRPeriod,
		ATRMultiplier:     s.ATRMultiplier,
		RewardRatio:       s.RewardRatio,
		RangeUseCap:       rangeUseCap,
		VolumeFilter:      s.VolumeFilterEnabled(),
		VolumeMinRatio:    s.VolumeMinRatio,
		BreakoutThreshold: s.BreakoutThreshold,
	}
	return opts
}

// CostsConfig возвращает модель издержек с учётом class_code.
func (c *Config) CostsConfig() costs.Config {
	return c.Costs
}

// CommissionPerLot возвращает flat round-trip (legacy API; 0 при rate-модели).
func (c *Config) CommissionPerLot() float64 {
	return c.Costs.PerLot(c.ClassCode)
}

// TrailingConfig конвертирует параметры трейлинга из YAML в trailing.Config.
func (s StrategyConfig) TrailingConfig(stepPriceValue float64, costsCfg costs.Config, classCode string) trailing.Config {
	cfg := trailing.DefaultConfig()
	cfg.StepPriceValue = stepPriceValue
	cfg.Costs = costsCfg
	cfg.ClassCode = classCode
	if s.TrailActivationR > 0 {
		cfg.ActivationR = s.TrailActivationR
	}
	if s.TrailDiscreteStepR > 0 {
		cfg.DiscreteStepR = s.TrailDiscreteStepR
	}
	if s.TrailStageMax > 0 {
		cfg.StageMax = s.TrailStageMax
	}
	if s.TrailBreakevenR > 0 {
		cfg.BreakevenR = s.TrailBreakevenR
	}
	return cfg
}

func (s StrategyConfig) VolumeFilterEnabled() bool {
	if s.VolumeFilter == nil {
		return false
	}
	return *s.VolumeFilter
}

// Load читает YAML-конфиг с диска и применяет значения по умолчанию.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("чтение конфига %q: %w", path, err)
	}
	return LoadFromBytes(data)
}

// LoadFromBytes разбирает YAML-конфиг из байтов (для тестов).
func LoadFromBytes(data []byte) (*Config, error) {
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("разбор конфига: %w", err)
	}

	cfg.applyDefaults()
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func (c *Config) applyDefaults() {
	c.TradingMode = strings.ToLower(strings.TrimSpace(c.TradingMode))
	if c.TradingMode == "" {
		c.TradingMode = TradingModeVirtual
	}

	if c.ClassCode == "" {
		c.ClassCode = defaultClassCode
	}
	if c.CandleTimeFrame == "" {
		c.CandleTimeFrame = defaultCandleTimeFrame
	}

	c.applyRiskDefaults(&c.Risk, c.Risk.Deposit)
	c.applyStrategyDefaults(&c.Strategy)
	if c.Virtual.Balance <= 0 {
		c.Virtual.Balance = c.Risk.Deposit
	}

	if c.Session.Timezone == "" {
		c.Session.Timezone = defaultTimezone
	}
	if c.Session.EODCloseTime == "" {
		c.Session.EODCloseTime = defaultEODCloseTime
	}
	if c.Session.SessionOpenTime == "" {
		c.Session.SessionOpenTime = defaultSessionOpenTime
	}

	if c.Storage.Path == "" {
		c.Storage.Path = defaultStoragePath
	}

	for i := range c.Tickers {
		if c.Tickers[i].StepPriceValue <= 0 {
			c.Tickers[i].StepPriceValue = defaultStepPriceValue
		}
	}

	for i := range c.Experiments {
		exp := &c.Experiments[i]
		exp.ID = strings.TrimSpace(exp.ID)
		if exp.Name == "" {
			exp.Name = exp.ID
		}
		c.applyRiskDefaults(&exp.Risk, exp.Risk.Deposit)
		c.applyStrategyDefaults(&exp.Strategy)
		if exp.Virtual.Balance <= 0 {
			exp.Virtual.Balance = exp.Risk.Deposit
		}
		for j := range exp.Tickers {
			if exp.Tickers[j].StepPriceValue <= 0 {
				exp.Tickers[j].StepPriceValue = defaultStepPriceValue
			}
		}
	}
}

func (c *Config) applyRiskDefaults(risk *RiskConfig, depositHint float64) {
	if risk.Deposit <= 0 {
		if depositHint > 0 {
			risk.Deposit = depositHint
		} else {
			risk.Deposit = defaultDeposit
		}
	}
	if risk.MaxDailyLoss <= 0 {
		pct := risk.MaxDailyLossPercent
		if pct <= 0 {
			pct = defaultMaxDailyLossPct
		}
		risk.MaxDailyLoss = risk.Deposit * pct / 100
	}
	if risk.RiskPerTradePercent <= 0 {
		risk.RiskPerTradePercent = defaultRiskPerTradePct
	}
	if risk.MaxParallelTrades <= 0 {
		risk.MaxParallelTrades = 2
	}
}

func (c *Config) applyStrategyDefaults(s *StrategyConfig) {
	if s.Lookback < 2 {
		s.Lookback = defaultLookback
	}
	s.StopMode = strings.ToLower(strings.TrimSpace(s.StopMode))
	if s.StopMode == "" {
		s.StopMode = strategy.StopModeRange
	}
	if s.ATRPeriod < 2 {
		s.ATRPeriod = defaultATRPeriod
	}
	if s.ATRMultiplier <= 0 {
		s.ATRMultiplier = defaultATRMultiplier
	}
}

func (c *Config) validate() error {
	switch c.TradingMode {
	case TradingModeVirtual, TradingModeReal:
	default:
		return fmt.Errorf("неверный trading_mode %q (допустимо: virtual, real)", c.TradingMode)
	}

	if len(c.Tickers) == 0 {
		return fmt.Errorf("список tickers не может быть пустым")
	}

	for _, t := range c.Tickers {
		if t.Symbol == "" {
			return fmt.Errorf("в tickers есть пустое значение")
		}
		if t.StepPriceValue <= 0 {
			return fmt.Errorf("tickers.%s: step_price_value должен быть > 0", t.Symbol)
		}
	}

	if c.HasExperiments() {
		if c.TradingMode == TradingModeReal && len(c.Experiments) > 1 {
			return fmt.Errorf("real mode: допускается не более одного эксперимента")
		}
		seen := make(map[string]struct{}, len(c.Experiments))
		for _, exp := range c.Experiments {
			if exp.ID == "" {
				return fmt.Errorf("experiments: id обязателен")
			}
			if _, dup := seen[exp.ID]; dup {
				return fmt.Errorf("experiments: дублирующийся id %q", exp.ID)
			}
			seen[exp.ID] = struct{}{}
			if err := validateStrategyConfig(exp.Strategy); err != nil {
				return fmt.Errorf("experiments.%s: %w", exp.ID, err)
			}
			if !c.Portfolio.SharedAccount {
				if exp.Risk.Deposit <= 0 {
					return fmt.Errorf("experiments.%s: risk.deposit должен быть > 0", exp.ID)
				}
				if exp.Risk.MaxDailyLoss <= 0 {
					return fmt.Errorf("experiments.%s: risk.max_daily_loss должен быть > 0", exp.ID)
				}
			}
			for _, t := range exp.Tickers {
				if t.Symbol == "" {
					return fmt.Errorf("experiments.%s: пустой тикер", exp.ID)
				}
			}
		}
		if c.Portfolio.SharedAccount {
			if c.Risk.Deposit <= 0 {
				return fmt.Errorf("portfolio.shared_account: корневой risk.deposit должен быть > 0")
			}
			if c.Risk.MaxDailyLoss <= 0 {
				return fmt.Errorf("portfolio.shared_account: корневой risk.max_daily_loss должен быть > 0")
			}
		}
		return nil
	}

	if err := validateStrategyConfig(c.Strategy); err != nil {
		return err
	}
	if c.Risk.Deposit <= 0 {
		return fmt.Errorf("risk.deposit должен быть > 0")
	}
	if c.Risk.MaxDailyLoss <= 0 {
		return fmt.Errorf("risk.max_daily_loss должен быть > 0")
	}

	return nil
}

func validateStrategyConfig(s StrategyConfig) error {
	if err := ValidateStrategyType(s.TypeOrDefault()); err != nil {
		return err
	}
	if s.StopMode == "" {
		return nil
	}
	switch s.StopMode {
	case strategy.StopModeRange, strategy.StopModeATR:
	default:
		return fmt.Errorf("неверный stop_mode %q (допустимо: range, atr)", s.StopMode)
	}
	return nil
}
