package config

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	TradingModeVirtual = "virtual"
	TradingModeReal    = "real"

	defaultClassCode       = "TQBR"
	defaultCandleTimeFrame = "M5"
	defaultDeposit         = 100_000
	defaultLookback        = 20
	defaultMaxDailyLossPct = 2.0
	defaultRiskPerTradePct = 0.5
	defaultTimezone        = "Europe/Moscow"
	defaultEODCloseTime    = "23:40"
	defaultSessionOpenTime = "10:00"
)

// Config описывает все настройки бота, кроме секретов (токен — только из env).
type Config struct {
	TradingMode     string         `yaml:"trading_mode"`
	Tickers         []string       `yaml:"tickers"`
	ClassCode       string         `yaml:"class_code"`
	CandleTimeFrame string         `yaml:"candle_timeframe"`
	Risk            RiskConfig     `yaml:"risk"`
	Strategy        StrategyConfig `yaml:"strategy"`
	Virtual         VirtualConfig  `yaml:"virtual"`
	Session         SessionConfig  `yaml:"session"`
}

type RiskConfig struct {
	Deposit             float64 `yaml:"deposit"`
	MaxDailyLoss        float64 `yaml:"max_daily_loss"`
	MaxDailyLossPercent float64 `yaml:"max_daily_loss_percent"`
	RiskPerTradePercent float64 `yaml:"risk_per_trade_percent"`
}

type StrategyConfig struct {
	Lookback int `yaml:"lookback"`
}

type VirtualConfig struct {
	Balance float64 `yaml:"balance"`
}

type SessionConfig struct {
	Timezone        string `yaml:"timezone"`
	EODCloseTime    string `yaml:"eod_close_time"`
	SessionOpenTime string `yaml:"session_open_time"`
}

// Load читает YAML-конфиг с диска и применяет значения по умолчанию.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("чтение конфига %q: %w", path, err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("разбор конфига %q: %w", path, err)
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

	if c.Risk.Deposit <= 0 {
		c.Risk.Deposit = defaultDeposit
	}
	if c.Risk.MaxDailyLoss <= 0 {
		pct := c.Risk.MaxDailyLossPercent
		if pct <= 0 {
			pct = defaultMaxDailyLossPct
		}
		c.Risk.MaxDailyLoss = c.Risk.Deposit * pct / 100
	}
	if c.Risk.RiskPerTradePercent <= 0 {
		c.Risk.RiskPerTradePercent = defaultRiskPerTradePct
	}

	if c.Strategy.Lookback < 2 {
		c.Strategy.Lookback = defaultLookback
	}

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

	for i, t := range c.Tickers {
		c.Tickers[i] = strings.TrimSpace(strings.ToUpper(t))
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
		if t == "" {
			return fmt.Errorf("в tickers есть пустое значение")
		}
	}

	if c.Risk.Deposit <= 0 {
		return fmt.Errorf("risk.deposit должен быть > 0")
	}
	if c.Risk.MaxDailyLoss <= 0 {
		return fmt.Errorf("risk.max_daily_loss должен быть > 0")
	}

	return nil
}

// PerTickerDeposit возвращает депозит, выделенный на один тикер.
func (c *Config) PerTickerDeposit() float64 {
	return c.Risk.Deposit / float64(len(c.Tickers))
}

// PerTickerMaxDailyLoss возвращает дневной лимит убытка на один тикер.
func (c *Config) PerTickerMaxDailyLoss() float64 {
	return c.Risk.MaxDailyLoss / float64(len(c.Tickers))
}
