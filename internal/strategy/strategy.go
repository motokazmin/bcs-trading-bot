package strategy

import "bcs-trading-bot/pkg/models"

const (
	IDMomentumBreakout  = "momentum_breakout"
	IDMomentumFiltered  = "momentum_filtered"
	IDOpeningRange      = "opening_range"
	IDMeanReversion     = "mean_reversion"
)

// CandleStrategy принимает свечу и возвращает сигнал на вход или nil.
type CandleStrategy interface {
	ID() string
	OnCandle(candle models.Candle) *models.Order
}

// DefaultType возвращает тип стратегии по умолчанию.
func DefaultType() string {
	return IDMomentumBreakout
}
