package strategy

import "bcs-trading-bot/pkg/models"

const (
	IDMomentumBreakout            = "momentum_breakout"
	IDMomentumFiltered            = "momentum_filtered"
	IDOpeningRange                = "opening_range"
	IDOpeningRangeContinuation    = "opening_range_continuation"
	IDOpeningRangeFade            = "opening_range_fade"
	IDMeanReversion               = "mean_reversion"
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

// DefaultRewardRatio — R:R take-profit при отсутствии reward_ratio в конфиге.
func DefaultRewardRatio(typeID string) float64 {
	switch typeID {
	case IDMeanReversion:
		return 1.5
	case IDOpeningRange:
		return 2.0
	case IDOpeningRangeContinuation:
		return 2.60
	case IDOpeningRangeFade:
		return 1.5
	case IDMomentumFiltered:
		return 2.0
	default:
		return 3.0
	}
}
