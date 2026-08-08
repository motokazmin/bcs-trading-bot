package position

import (
	"math"
	"time"

	"bcs-trading-bot/pkg/models"
)

// State — открытая позиция (общая для live-воркера и backtest-симулятора).
type State struct {
	Direction         string
	Quantity          int
	EntryPrice        float64
	InitialStopLoss   float64
	InitialTakeProfit float64
	StopLoss          float64
	TakeProfit        float64
	RDistance         float64
	TrailStage        int
	MFEPrice          float64
	MAEPrice          float64
	BreakoutUpper     float64
	BreakoutLower     float64
	OpenedAt          time.Time
	EntryBarTime      time.Time // метка M5-бара входа
	EntryBarClose     float64   // close бара входа (для audit)
	SameBarExit       bool      // закрытие same-bar после limit-fill
}

// NewFromSignal создаёт состояние позиции из исполненного сигнала.
func NewFromSignal(signal models.Order, openedAt time.Time) *State {
	return &State{
		Direction:         signal.Direction,
		Quantity:          signal.Quantity,
		EntryPrice:        signal.Price,
		InitialStopLoss:   signal.StopLoss,
		InitialTakeProfit: signal.TakeProfit,
		StopLoss:          signal.StopLoss,
		TakeProfit:        signal.TakeProfit,
		RDistance:         math.Abs(signal.Price - signal.StopLoss),
		TrailStage:        0,
		MFEPrice:          signal.Price,
		MAEPrice:          signal.Price,
		BreakoutUpper:     signal.BreakoutUpper,
		BreakoutLower:     signal.BreakoutLower,
		OpenedAt:          openedAt,
	}
}

func UpdateMFE(pos *State, price float64) {
	if pos == nil {
		return
	}
	switch pos.Direction {
	case "BUY":
		if price > pos.MFEPrice {
			pos.MFEPrice = price
		}
	case "SELL":
		if price < pos.MFEPrice {
			pos.MFEPrice = price
		}
	}
}

func UpdateMAE(pos *State, price float64) {
	if pos == nil {
		return
	}
	switch pos.Direction {
	case "BUY":
		if price < pos.MAEPrice {
			pos.MAEPrice = price
		}
	case "SELL":
		if price > pos.MAEPrice {
			pos.MAEPrice = price
		}
	}
}

// CalcPnL возвращает gross PnL в рублях (комиссия не вычитается).
func CalcPnL(pos *State, closePrice, stepPriceValue float64) float64 {
	if pos == nil {
		return 0
	}
	qty := float64(pos.Quantity)
	switch pos.Direction {
	case "BUY":
		return (closePrice - pos.EntryPrice) * qty * stepPriceValue
	case "SELL":
		return (pos.EntryPrice - closePrice) * qty * stepPriceValue
	default:
		return 0
	}
}

func CalcMFEinR(pos *State) float64 {
	if pos == nil || pos.RDistance <= 0 {
		return 0
	}
	switch pos.Direction {
	case "BUY":
		return (pos.MFEPrice - pos.EntryPrice) / pos.RDistance
	case "SELL":
		return (pos.EntryPrice - pos.MFEPrice) / pos.RDistance
	default:
		return 0
	}
}

func CalcMAEinR(pos *State) float64 {
	if pos == nil || pos.RDistance <= 0 {
		return 0
	}
	switch pos.Direction {
	case "BUY":
		return (pos.EntryPrice - pos.MAEPrice) / pos.RDistance
	case "SELL":
		return (pos.MAEPrice - pos.EntryPrice) / pos.RDistance
	default:
		return 0
	}
}

// CheckExit проверяет SL/TP на заданной цене. Возвращает причину закрытия или "".
func CheckExit(pos *State, price float64) string {
	if pos == nil {
		return ""
	}
	switch pos.Direction {
	case "BUY":
		if price <= pos.StopLoss {
			return models.CloseReasonStopLoss
		}
		if price >= pos.TakeProfit {
			return models.CloseReasonTakeProfit
		}
	case "SELL":
		if price >= pos.StopLoss {
			return models.CloseReasonStopLoss
		}
		if price <= pos.TakeProfit {
			return models.CloseReasonTakeProfit
		}
	}
	return ""
}

// ExitFillPrice — цена исполнения выхода в paper/virtual.
// SL/TP исполняются по уровню (без adverse tick за стопом); EOD и прочее — по marketPrice.
func ExitFillPrice(pos *State, reason string, marketPrice float64) float64 {
	if pos == nil {
		return marketPrice
	}
	switch reason {
	case models.CloseReasonStopLoss:
		if pos.StopLoss > 0 {
			return pos.StopLoss
		}
	case models.CloseReasonTakeProfit:
		if pos.TakeProfit > 0 {
			return pos.TakeProfit
		}
	}
	return marketPrice
}

// SameBarExitAfterFill — после limit-fill внутри бара (entry ≠ close): пробит ли SL/TP по OHLC.
// Для входа по close (fade/MF и т.п.) возвращает "" — wick до закрытия бара ещё не «в позиции».
// Консервативно: при касании обоих сначала STOP_LOSS (как у TATN: High ушёл далеко за стоп).
func SameBarExitAfterFill(pos *State, candle models.Candle) string {
	if pos == nil || pos.EntryPrice <= 0 {
		return ""
	}
	// Вход по цене закрытия бара — same-bar OHLC до entry не применяем.
	if pricesEqual(pos.EntryPrice, candle.Close) {
		return ""
	}
	switch pos.Direction {
	case "BUY":
		if pos.StopLoss > 0 && candle.Low <= pos.StopLoss {
			return models.CloseReasonStopLoss
		}
		if pos.TakeProfit > 0 && candle.High >= pos.TakeProfit {
			return models.CloseReasonTakeProfit
		}
	case "SELL":
		if pos.StopLoss > 0 && candle.High >= pos.StopLoss {
			return models.CloseReasonStopLoss
		}
		if pos.TakeProfit > 0 && candle.Low <= pos.TakeProfit {
			return models.CloseReasonTakeProfit
		}
	}
	return ""
}

func pricesEqual(a, b float64) bool {
	d := math.Abs(a - b)
	if d < 1e-9 {
		return true
	}
	scale := math.Max(math.Abs(a), math.Abs(b))
	return scale > 0 && d/scale < 1e-12
}

// IntrabarPrices возвращает синтетический путь цены внутри свечи для проверки SL/TP.
func IntrabarPrices(candle models.Candle, direction string) []float64 {
	switch direction {
	case "BUY":
		return []float64{candle.Open, candle.Low, candle.High, candle.Close}
	case "SELL":
		return []float64{candle.Open, candle.High, candle.Low, candle.Close}
	default:
		return []float64{candle.Close}
	}
}
