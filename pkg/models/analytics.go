package models

import "time"

// TradeFilter задаёт фильтры выборки сделок.
type TradeFilter struct {
	ExperimentID string
	Ticker       string
	TradingMode  string
	RunID        string
	DateFrom     string // YYYY-MM-DD
	DateTo       string
	CloseReason  string
}

// TradeListResult — страница сделок.
type TradeListResult struct {
	Trades []ClosedTrade
	Total  int
}

// TradeSummary — агрегированные метрики по выборке.
type TradeSummary struct {
	TradeCount    int     `json:"trade_count"`
	WinCount      int     `json:"win_count"`
	LossCount     int     `json:"loss_count"`
	WinRate       float64 `json:"win_rate"`
	TotalPnL      float64 `json:"total_pnl"`
	AvgPnL        float64 `json:"avg_pnl"`
	AvgPnLR       float64 `json:"avg_pnl_r"`
	ProfitFactor  float64 `json:"profit_factor"`
	Expectancy    float64 `json:"expectancy"`
	AvgHoldSec    float64 `json:"avg_hold_seconds"`
	BestTradePnL  float64 `json:"best_trade_pnl"`
	WorstTradePnL float64 `json:"worst_trade_pnl"`
}

// BreakdownRow — метрики по одному ключу группировки.
type BreakdownRow struct {
	Key         string  `json:"key"`
	TradeCount  int     `json:"trade_count"`
	WinRate     float64 `json:"win_rate"`
	TotalPnL    float64 `json:"total_pnl"`
	AvgPnLR     float64 `json:"avg_pnl_r"`
	ProfitFactor float64 `json:"profit_factor"`
}

// DailyPnLRow — дневной результат.
type DailyPnLRow struct {
	TradingDate string  `json:"trading_date"`
	TradeCount  int     `json:"trade_count"`
	TotalPnL    float64 `json:"total_pnl"`
	WinRate     float64 `json:"win_rate"`
}

// EquityPoint — точка кривой эквити.
type EquityPoint struct {
	ClosedAt    time.Time `json:"closed_at"`
	CumulativePnL float64 `json:"cumulative_pnl"`
}

// ExperimentReport — полный отчёт по одному эксперименту.
type ExperimentReport struct {
	ExperimentID string         `json:"experiment_id"`
	StopMode     string         `json:"stop_mode"`
	Summary      TradeSummary   `json:"summary"`
	ByTicker     []BreakdownRow `json:"by_ticker"`
	ByCloseReason []BreakdownRow `json:"by_close_reason"`
	DailyPnL     []DailyPnLRow  `json:"daily_pnl"`
	EquityCurve  []EquityPoint  `json:"equity_curve"`
	Trades       []ClosedTrade  `json:"trades"`
}

// StrategyContext — описание стратегии для ИИ.
type StrategyContext struct {
	Name              string `json:"name"`
	Philosophy        string `json:"philosophy"`
	SignalLogic       string `json:"signal_logic"`
	RiskReward        string `json:"risk_reward"`
	RiskPerTrade      string `json:"risk_per_trade"`
	TrailingStop      string `json:"trailing_stop"`
	CircuitBreaker    string `json:"circuit_breaker"`
	PnLNote           string `json:"pnl_note"`
	ExperimentNote    string `json:"experiment_note"`
}

// AIExportBundle — пакет для выгрузки и анализа ИИ.
type AIExportBundle struct {
	ExportVersion   string             `json:"export_version"`
	ExportedAt      time.Time          `json:"exported_at"`
	Filters         TradeFilter        `json:"filters"`
	DateRange       DateRange          `json:"date_range"`
	StrategyContext StrategyContext    `json:"strategy_context"`
	Experiments     []ExperimentReport `json:"experiments"`
	Comparison      []BreakdownRow     `json:"comparison"`
	Prompt          string             `json:"prompt"`
}

// DateRange — фактический диапазон дат в выборке.
type DateRange struct {
	From string `json:"from"`
	To   string `json:"to"`
}
