package models

import "time"

// PositionSnapshot — нейтральный снимок открытой позиции (без знания о HTTP).
type PositionSnapshot struct {
	ID            string    `json:"id"`
	ExperimentID  string    `json:"experiment_id"`
	Ticker        string    `json:"ticker"`
	Direction     string    `json:"direction"`
	Quantity      int       `json:"quantity"`
	EntryPrice    float64   `json:"entry_price"`
	StopLoss      float64   `json:"stop_loss"`
	TakeProfit    float64   `json:"take_profit"`
	TrailStage    int       `json:"trail_stage"`
	OpenedAt      time.Time `json:"opened_at"`
	LastPrice     float64   `json:"last_price"`
	UnrealizedPnL float64   `json:"unrealized_pnl"`
	RDistance     float64   `json:"r_distance"`
	StepPrice     float64   `json:"step_price"`
}
