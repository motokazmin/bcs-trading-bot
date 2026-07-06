package trailing

import (
	"bcs-trading-bot/internal/position"
)

const defaultCommissionPerLot = 5.0

// Config задаёт параметры дискретного и непрерывного трейлинг-стопа.
type Config struct {
	ActivationR      float64 // порог первой стадии в единицах R (default 1.0)
	DiscreteStepR    float64 // шаг между стадиями в R (default 1.0)
	StageMax         int     // макс. дискретная стадия (default 2)
	CommissionPerLot float64 // комиссия за лот для offset безубытка (default 5.0)
	StepPriceValue   float64 // стоимость шага цены
}

// DefaultConfig возвращает Variant C (+1R безубыток, +2R фиксация, затем MFE-1R).
func DefaultConfig() Config {
	return Config{
		ActivationR:      1.0,
		DiscreteStepR:    1.0,
		StageMax:         2,
		CommissionPerLot: defaultCommissionPerLot,
		StepPriceValue:   1.0,
	}
}

func (c Config) normalized() Config {
	out := c
	if out.ActivationR <= 0 {
		out.ActivationR = 1.0
	}
	if out.DiscreteStepR <= 0 {
		out.DiscreteStepR = 1.0
	}
	if out.StageMax < 1 {
		out.StageMax = 2
	}
	if out.CommissionPerLot <= 0 {
		out.CommissionPerLot = defaultCommissionPerLot
	}
	if out.StepPriceValue <= 0 {
		out.StepPriceValue = 1.0
	}
	return out
}

// Apply обновляет stop-loss позиции по текущей цене.
func Apply(pos *position.State, price float64, cfg Config) {
	cfg = cfg.normalized()
	if pos == nil || pos.RDistance <= 0 {
		return
	}

	breakevenOffset := cfg.CommissionPerLot / cfg.StepPriceValue

	for stage := 1; stage <= cfg.StageMax; stage++ {
		if pos.TrailStage >= stage {
			continue
		}
		triggerR := cfg.ActivationR + float64(stage-1)*cfg.DiscreteStepR
		if !triggerReached(pos, price, triggerR) {
			continue
		}

		var newSL float64
		if stage == 1 {
			newSL = breakevenSL(pos, breakevenOffset)
		} else {
			lockR := cfg.ActivationR + float64(stage-2)*cfg.DiscreteStepR
			newSL = lockSL(pos, lockR)
		}
		moveSL(pos, newSL)
		pos.TrailStage = stage
	}

	if pos.TrailStage >= cfg.StageMax {
		continuousSL(pos)
	}
}

func triggerReached(pos *position.State, price, triggerR float64) bool {
	threshold := triggerR * pos.RDistance
	switch pos.Direction {
	case "BUY":
		return price >= pos.EntryPrice+threshold
	case "SELL":
		return price <= pos.EntryPrice-threshold
	default:
		return false
	}
}

func breakevenSL(pos *position.State, offset float64) float64 {
	switch pos.Direction {
	case "BUY":
		return pos.EntryPrice + offset
	case "SELL":
		return pos.EntryPrice - offset
	default:
		return pos.StopLoss
	}
}

func lockSL(pos *position.State, lockR float64) float64 {
	switch pos.Direction {
	case "BUY":
		return pos.EntryPrice + lockR*pos.RDistance
	case "SELL":
		return pos.EntryPrice - lockR*pos.RDistance
	default:
		return pos.StopLoss
	}
}

func moveSL(pos *position.State, newSL float64) {
	switch pos.Direction {
	case "BUY":
		if newSL > pos.StopLoss {
			pos.StopLoss = newSL
		}
	case "SELL":
		if newSL < pos.StopLoss {
			pos.StopLoss = newSL
		}
	}
}

func continuousSL(pos *position.State) {
	switch pos.Direction {
	case "BUY":
		newSL := pos.MFEPrice - pos.RDistance
		moveSL(pos, newSL)
	case "SELL":
		newSL := pos.MFEPrice + pos.RDistance
		moveSL(pos, newSL)
	}
}
