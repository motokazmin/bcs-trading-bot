package models

import "time"

const (
	OrderTypeLimit  = "LIMIT"
	OrderTypeMarket = "MARKET"
)

const (
	CloseReasonStopLoss   = "STOP_LOSS"
	CloseReasonTakeProfit = "TAKE_PROFIT"
	CloseReasonEOD        = "EOD"
)

type Candle struct {
	Ticker    string
	Open      float64
	High      float64
	Low       float64
	Close     float64
	Volume    int64
	Timestamp time.Time
}

// Tick — снимок последней цены сделки из потока котировок.
type Tick struct {
	Ticker    string
	Price     float64
	Timestamp time.Time
}

type Order struct {
	Ticker      string
	Direction   string
	Quantity    int
	Price       float64
	StopLoss    float64
	TakeProfit  float64
	OrderType   string
	CloseReason string
}
