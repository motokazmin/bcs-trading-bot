package config

import (
	"fmt"
	"os"
	"strings"

	"bcs-trading-bot/internal/strategy"

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
	Risk            RiskConfig         `yaml:"risk"`
	Strategy        StrategyConfig     `yaml:"strategy"`
	Virtual         VirtualConfig      `yaml:"virtual"`
	Session         SessionConfig      `yaml:"session"`
	Storage         StorageConfig      `yaml:"storage"`
	Experiments     []ExperimentConfig `yaml:"experiments"`
}

// ExperimentConfig — изолированный виртуальный счёт с собственными параметрами стратегии и риска.
type ExperimentConfig struct {
	ID       string         `yaml:"id"`
	Name     string         `yaml:"name"`
	Strategy StrategyConfig `yaml:"strategy"`
	Risk     RiskConfig     `yaml:"risk"`
	Virtual  VirtualConfig  `yaml:"virtual"`
}

// ResolvedExperiment — нормализованный эксперимент, готовый к запуску воркеров.
type ResolvedExperiment struct {
	ID       string
	Name     string
	Strategy StrategyConfig
	Risk     RiskConfig
	Virtual  VirtualConfig
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
}

type StrategyConfig struct {
	Lookback      int     `yaml:"lookback"`
	StopMode      string  `yaml:"stop_mode"`
	ATRPeriod     int     `yaml:"atr_period"`
	ATRMultiplier float64 `yaml:"atr_multiplier"`
	RewardRatio   float64 `yaml:"reward_ratio"`
	RangeUseCap   *bool   `yaml:"range_use_cap"`
}

type VirtualConfig struct {
	Balance float64 `yaml:"balance"`
}

type SessionConfig struct {
	Timezone        string `yaml:"timezone"`
	EODCloseTime    string `yaml:"eod_close_time"`
	SessionOpenTime string `yaml:"session_open_time"`
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
			ID:       exp.ID,
			Name:     exp.Name,
			Strategy: exp.Strategy,
			Risk:     exp.Risk,
			Virtual:  exp.Virtual,
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

// StrategyOptions конвертирует конфиг стратегии в параметры MomentumBreakout.
func (s StrategyConfig) StrategyOptions() strategy.Options {
	rangeUseCap := true
	if s.RangeUseCap != nil {
		rangeUseCap = *s.RangeUseCap
	}

	opts := strategy.Options{
		Lookback:      s.Lookback,
		StopMode:      strings.ToLower(strings.TrimSpace(s.StopMode)),
		ATRPeriod:     s.ATRPeriod,
		ATRMultiplier: s.ATRMultiplier,
		RewardRatio:   s.RewardRatio,
		RangeUseCap:   rangeUseCap,
	}
	return opts
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
			if exp.Risk.Deposit <= 0 {
				return fmt.Errorf("experiments.%s: risk.deposit должен быть > 0", exp.ID)
			}
			if exp.Risk.MaxDailyLoss <= 0 {
				return fmt.Errorf("experiments.%s: risk.max_daily_loss должен быть > 0", exp.ID)
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
	switch s.StopMode {
	case strategy.StopModeRange, strategy.StopModeATR:
	default:
		return fmt.Errorf("неверный stop_mode %q (допустимо: range, atr)", s.StopMode)
	}
	return nil
}
