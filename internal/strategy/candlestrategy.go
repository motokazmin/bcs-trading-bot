package strategy

import "bcs-trading-bot/internal/models"

const (
	IDMomentumBreakout            = "momentum_breakout"
	IDMomentumFiltered            = "momentum_filtered"
	IDOpeningRange                = "opening_range"
	IDOpeningRangeContinuation    = "opening_range_continuation"
	IDOpeningRangeFade            = "opening_range_fade"
	IDMeanReversion               = "mean_reversion"
	IDVWAPPullbackContinuation    = "vwap_pullback_continuation"
	IDMomentumSberDaytrend        = "momentum_sber_daytrend"
	IDMiddayCompressionBreakout   = "midday_compression_breakout"
	IDLateSessionImbalance        = "late_session_imbalance"
	IDPrevDayLevelBreakout        = "prev_day_level_breakout"
	IDAfternoonRangeFade          = "afternoon_range_fade"
	IDSessionORC                  = "session_orc"
	IDSessionORFade               = "session_or_fade"
	IDSessionGapDrive             = "session_gap_drive"
	IDRandomEntry                 = "random_entry"
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
	case IDOpeningRangeContinuation, IDSessionORC:
		return 2.60
	case IDOpeningRangeFade, IDSessionORFade:
		return 1.5
	case IDSessionGapDrive:
		return 1.75
	case IDMomentumFiltered:
		return 2.0
	case IDVWAPPullbackContinuation:
		return 2.0
	case IDMomentumSberDaytrend:
		return 2.0
	case IDMiddayCompressionBreakout:
		return 2.25
	case IDLateSessionImbalance:
		return 1.75
	case IDPrevDayLevelBreakout:
		return 2.0
	case IDAfternoonRangeFade:
		return 1.5
	case IDRandomEntry:
		return 1.5
	default:
		return 3.0
	}
}
