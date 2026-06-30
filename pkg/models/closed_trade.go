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
	CloseReason       string
	TrailStage        int
	IsWinner          bool
	OpenedAt          time.Time
	ClosedAt          time.Time
	HoldSeconds       int
	TradingDate       string
	CandleTimeframe   string
	Lookback          int
	RiskPerTradePct   float64
	DepositPerTicker  float64
}
