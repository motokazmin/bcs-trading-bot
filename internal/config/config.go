package config

import (
	"fmt"
	"os"
	"strings"

	"bcs-trading-bot/internal/costs"
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
	Costs           costs.Config       `yaml:"costs"`
	Risk        RiskConfig         `yaml:"risk"`
	Strategy    StrategyConfig     `yaml:"strategy"`
	Virtual     VirtualConfig      `yaml:"virtual"`
	Session     SessionConfig      `yaml:"session"`
	Storage     StorageConfig      `yaml:"storage"`
	Experiments []ExperimentConfig `yaml:"experiments"`
	// Runtime — как и ExperimentConfig.Runtime, но для конфигов без явного
	// experiments: (одностратегийные, как configs/strategies/*.yaml) —
	// применяется к единственному "default"-эксперименту.
	Runtime string `yaml:"runtime"`
}

// ExperimentConfig — слот стратегии в портфеле (общий счёт на все experiments).
type ExperimentConfig struct {
	ID                string         `yaml:"id"`
	Name              string         `yaml:"name"`
	Tickers           []TickerConfig `yaml:"tickers"`
	EntryDelayMinutes *int           `yaml:"entry_delay_minutes"`
	SessionOpenTime   string         `yaml:"session_open_time"`
	EODCloseTime      string         `yaml:"eod_close_time"`
	WeekdaysOnly      *bool          `yaml:"weekdays_only"`
	WeekendOnly       *bool          `yaml:"weekend_only"`
	Strategy          StrategyConfig `yaml:"strategy"`
	Risk              RiskConfig     `yaml:"risk"`
	// CandleTimeframe — таймфрейм свечей для этого эксперимента (M1, M5, H1, ...).
	// Если пусто — используется корневой Config.CandleTimeFrame. Разные эксперименты
	// в одном процессе могут подписываться на разные таймфреймы одного или разных
	// тикеров (см. internal/datafeed) — это часть ADR 0001 "свобода стратегии".
	CandleTimeframe string `yaml:"candle_timeframe"`
	// Runtime — каркас эксперимента. Единственное поддерживаемое значение:
	// "strategy" — internal/engine.StrategyRunner поверх
	// internal/strategies/adapter.SelfManagedStrategy (ADR 0001, Фазы 3–5).
	Runtime string `yaml:"runtime"`
}

// ResolvedExperiment — нормализованный эксперимент, готовый к запуску воркеров.
type ResolvedExperiment struct {
	ID                string
	Name              string
	Tickers           []TickerConfig
	EntryDelayMinutes *int
	SessionOpenTime   string
	EODCloseTime      string
	WeekdaysOnly      *bool
	WeekendOnly       *bool
	Strategy          StrategyConfig
	Risk              RiskConfig
	// CandleTimeframe — эффективный таймфрейм эксперимента: exp.CandleTimeframe,
	// если задан, иначе Config.CandleTimeFrame (заполняется в ResolvedExperiments()).
	CandleTimeframe string
	// Runtime — см. ExperimentConfig.Runtime.
	Runtime string
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

type VirtualConfig struct {
	Balance float64 `yaml:"balance"`
}

type SessionConfig struct {
	Timezone          string `yaml:"timezone"`
	EODCloseTime      string `yaml:"eod_close_time"`
	SessionOpenTime   string `yaml:"session_open_time"`
	EntryDelayMinutes int    `yaml:"entry_delay_minutes"`
	WeekdaysOnly      bool   `yaml:"weekdays_only"`
	WeekendOnly       bool   `yaml:"weekend_only"`
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
			ID:              defaultExperimentID,
			Name:            defaultExperimentID,
			Strategy:        c.Strategy,
			Risk:            c.Risk,
			CandleTimeframe: c.CandleTimeFrame,
			Runtime:         strings.TrimSpace(c.Runtime),
		}}
	}

	out := make([]ResolvedExperiment, len(c.Experiments))
	for i, exp := range c.Experiments {
		timeframe := strings.TrimSpace(exp.CandleTimeframe)
		if timeframe == "" {
			timeframe = c.CandleTimeFrame
		}
		out[i] = ResolvedExperiment{
			ID:                exp.ID,
			Name:              exp.Name,
			Tickers:           exp.Tickers,
			EntryDelayMinutes: exp.EntryDelayMinutes,
			SessionOpenTime:   exp.SessionOpenTime,
			EODCloseTime:      exp.EODCloseTime,
			WeekdaysOnly:      exp.WeekdaysOnly,
			WeekendOnly:       exp.WeekendOnly,
			Strategy:          exp.Strategy,
			Risk:              exp.Risk,
			CandleTimeframe:   timeframe,
			Runtime:           strings.TrimSpace(exp.Runtime),
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
	if strings.TrimSpace(exp.SessionOpenTime) != "" {
		s.SessionOpenTime = strings.TrimSpace(exp.SessionOpenTime)
	}
	if strings.TrimSpace(exp.EODCloseTime) != "" {
		s.EODCloseTime = strings.TrimSpace(exp.EODCloseTime)
	}
	if exp.WeekdaysOnly != nil {
		s.WeekdaysOnly = *exp.WeekdaysOnly
	}
	if exp.WeekendOnly != nil {
		s.WeekendOnly = *exp.WeekendOnly
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

// AccountRisk — риск единого счёта (корневой risk; fallback — первый experiment).
func (c *Config) AccountRisk() RiskConfig {
	if c.Risk.Deposit > 0 {
		return c.Risk
	}
	exps := c.ResolvedExperiments()
	if len(exps) > 0 {
		return exps[0].Risk
	}
	return c.Risk
}

// AccountBalance — баланс единого VirtualExecutor.
func (c *Config) AccountBalance() float64 {
	if c.Virtual.Balance > 0 {
		return c.Virtual.Balance
	}
	return c.AccountRisk().Deposit
}

// CostsConfig возвращает модель издержек с учётом class_code.
func (c *Config) CostsConfig() costs.Config {
	return c.Costs
}

// CommissionPerLot возвращает flat round-trip (legacy API; 0 при rate-модели).
func (c *Config) CommissionPerLot() float64 {
	return c.Costs.PerLot(c.ClassCode)
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

	if c.HasExperiments() {
		// Корневой risk — единый счёт; не заполняем defaultDeposit, если не задан
		// (AccountRisk возьмёт fallback с первого experiment — legacy YAML).
		if c.Risk.Deposit > 0 || c.Risk.MaxDailyLoss > 0 || c.Risk.MaxDailyLossPercent > 0 {
			c.applyRiskDefaults(&c.Risk, c.Risk.Deposit)
		}
	} else {
		c.applyRiskDefaults(&c.Risk, c.Risk.Deposit)
	}
	c.Strategy.applyDefaults()
	if c.Virtual.Balance <= 0 && c.Risk.Deposit > 0 {
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
		exp.Strategy.applyDefaults()
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
			for _, t := range exp.Tickers {
				if t.Symbol == "" {
					return fmt.Errorf("experiments.%s: пустой тикер", exp.ID)
				}
			}
		}
		ar := c.AccountRisk()
		if ar.Deposit <= 0 {
			return fmt.Errorf("risk.deposit должен быть > 0 (единый счёт)")
		}
		if ar.MaxDailyLoss <= 0 {
			return fmt.Errorf("risk.max_daily_loss должен быть > 0 (единый счёт)")
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
