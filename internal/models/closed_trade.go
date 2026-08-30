package models

import "time"

// ClosedTrade — снимок закрытой позиции для анализа эффективности.
type ClosedTrade struct {
	TradingMode       string
	RunID             string
	ExperimentID      string
	StopMode          string
	Ticker            string
	ClassCode         string
	StepPriceValue    float64
	Direction         string
	Quantity          int
	EntryPrice        float64
	ExitPrice         float64
	InitialStopLoss   float64
	InitialTakeProfit float64
	FinalStopLoss     float64
	RDistance         float64
	GrossPnL          float64
	PnLR              float64
	MFEinR            float64 `json:"mfe_in_r"`
	MAEinR            float64 `json:"mae_in_r"`
	BreakoutUpper     float64 `json:"breakout_upper"`
	BreakoutLower     float64 `json:"breakout_lower"`
	CloseReason       string
	TrailStage        int
	IsWinner          bool
	OpenedAt          time.Time
	ClosedAt          time.Time
	HoldSeconds       int
	TradingDate       string
	CandleTimeframe   string
	Lookback           int
	RiskPerTradePct    float64
	DepositPerTicker   float64
	StrategyParamsJSON string `json:"strategy_params,omitempty"`
	AuditSeverity      string `json:"audit_severity,omitempty"`
	AuditCodes         string `json:"audit_codes,omitempty"`
	EntryBarTime       string `json:"entry_bar_time,omitempty"`
	EntryBarClose      float64 `json:"entry_bar_close,omitempty"`
}

func (t ClosedTrade) effectiveStepPrice() float64 {
	if t.StepPriceValue > 0 {
		return t.StepPriceValue
	}
	return 1
}

// LotValueRub — стоимость одного лота в рублях на цене входа.
func (t ClosedTrade) LotValueRub() float64 {
	return t.EntryPrice * t.effectiveStepPrice()
}

// NotionalRub — общая сумма позиции на входе в рублях.
func (t ClosedTrade) NotionalRub() float64 {
	return t.LotValueRub() * float64(t.Quantity)
}
