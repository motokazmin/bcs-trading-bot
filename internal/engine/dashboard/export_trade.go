package dashboard

import "bcs-trading-bot/internal/models"

// ExportTrade — сделка в выгрузке разбора. Имена полей ОДИН-В-ОДИН с колонками
// closed_trades: scripts/analyze-trades.py читает и БД, и этот JSON одним кодом,
// без таблицы соответствий, которая разъехалась бы на первой же новой колонке.
//
// Отдельный тип, а не теги на models.ClosedTrade: админка местами читает поля в
// PascalCase (`/api/trades`), и переименование сломало бы фронт.
//
// Зеркало проверяется тестом против PRAGMA table_info — добавил колонку в БД,
// но не сюда, и тест упадёт.
type ExportTrade struct {
	ID                int64   `json:"id"`
	TradingMode       string  `json:"trading_mode"`
	RunID             string  `json:"run_id"`
	RecordedAt        string  `json:"recorded_at"`
	Ticker            string  `json:"ticker"`
	ClassCode         string  `json:"class_code"`
	StepPriceValue    float64 `json:"step_price_value"`
	Direction         string  `json:"direction"`
	Quantity          int     `json:"quantity"`
	EntryPrice        float64 `json:"entry_price"`
	ExitPrice         float64 `json:"exit_price"`
	InitialStopLoss   float64 `json:"initial_stop_loss"`
	InitialTakeProfit float64 `json:"initial_take_profit"`
	FinalStopLoss     float64 `json:"final_stop_loss"`
	RDistance         float64 `json:"r_distance"`
	GrossPnL          float64 `json:"gross_pnl"`
	PnLR              float64 `json:"pnl_r"`
	CloseReason       string  `json:"close_reason"`
	TrailStage        int     `json:"trail_stage"`
	IsWinner          int     `json:"is_winner"`
	OpenedAt          string  `json:"opened_at"`
	ClosedAt          string  `json:"closed_at"`
	HoldSeconds       int     `json:"hold_seconds"`
	TradingDate       string  `json:"trading_date"`
	CandleTimeframe   string  `json:"candle_timeframe"`
	Lookback          int     `json:"lookback"`
	RiskPerTradePct   float64 `json:"risk_per_trade_pct"`
	DepositPerTicker  float64 `json:"deposit_per_ticker"`
	ExperimentID      string  `json:"experiment_id"`
	StopMode          string  `json:"stop_mode"`
	MFEinR            float64 `json:"mfe_in_r"`
	MAEinR            float64 `json:"mae_in_r"`
	BreakoutUpper     float64 `json:"breakout_upper"`
	BreakoutLower     float64 `json:"breakout_lower"`
	AuditSeverity     string  `json:"audit_severity"`
	AuditCodes        string  `json:"audit_codes"`
	EntryBarTime      string  `json:"entry_bar_time"`
	EntryBarClose     float64 `json:"entry_bar_close"`
	RequestedQuantity int     `json:"requested_quantity"`
	CashAtOpen        float64 `json:"cash_at_open"`
	BarAgeSeconds     float64 `json:"bar_age_seconds"`
}

// dbTimeLayout — формат времени в closed_trades (московское wall-time).
const dbTimeLayout = "2006-01-02 15:04:05"

func toExportTrade(t models.ClosedTrade) ExportTrade {
	winner := 0
	if t.IsWinner {
		winner = 1
	}
	return ExportTrade{
		ID: t.ID, TradingMode: t.TradingMode, RunID: t.RunID, RecordedAt: t.RecordedAt,
		Ticker: t.Ticker, ClassCode: t.ClassCode, StepPriceValue: t.StepPriceValue,
		Direction: t.Direction, Quantity: t.Quantity,
		EntryPrice: t.EntryPrice, ExitPrice: t.ExitPrice,
		InitialStopLoss: t.InitialStopLoss, InitialTakeProfit: t.InitialTakeProfit,
		FinalStopLoss: t.FinalStopLoss, RDistance: t.RDistance,
		GrossPnL: t.GrossPnL, PnLR: t.PnLR,
		CloseReason: t.CloseReason, TrailStage: t.TrailStage, IsWinner: winner,
		OpenedAt:    t.OpenedAt.Format(dbTimeLayout),
		ClosedAt:    t.ClosedAt.Format(dbTimeLayout),
		HoldSeconds: t.HoldSeconds, TradingDate: t.TradingDate,
		CandleTimeframe: t.CandleTimeframe, Lookback: t.Lookback,
		RiskPerTradePct: t.RiskPerTradePct, DepositPerTicker: t.DepositPerTicker,
		ExperimentID: t.ExperimentID, StopMode: t.StopMode,
		MFEinR: t.MFEinR, MAEinR: t.MAEinR,
		BreakoutUpper: t.BreakoutUpper, BreakoutLower: t.BreakoutLower,
		AuditSeverity: t.AuditSeverity, AuditCodes: t.AuditCodes,
		EntryBarTime: t.EntryBarTime, EntryBarClose: t.EntryBarClose,
		RequestedQuantity: t.RequestedQuantity, CashAtOpen: t.CashAtOpen,
		BarAgeSeconds: t.BarAgeSeconds,
	}
}
