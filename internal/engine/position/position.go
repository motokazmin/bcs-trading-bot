package position

import (
	"math"
	"time"

	"bcs-trading-bot/internal/models"
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
	// EntryAtBarClose — вход был по цене закрытия бара (fade/MF), а не лимитом
	// внутри бара. Признак ставится явно: выводить его сравнением EntryPrice и
	// candle.Close нельзя — проскальзывание сдвигает цену фила и сравнение врёт.
	EntryAtBarClose bool
	SameBarExit     bool // закрытие same-bar после limit-fill
	// RequestedQuantity — объём до капа по кэшу; CashAtOpen — свободный кэш на
	// момент расчёта; BarAgeSeconds — возраст свечи входа. Ставятся вызывающим
	// кодом сразу после открытия и едут в ClosedTrade (см. models.ClosedTrade).
	RequestedQuantity int
	CashAtOpen        float64
	BarAgeSeconds     float64
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
	// TakeProfit == 0 — тейк отключён (выход только по трейлингу/EOD).
	switch pos.Direction {
	case "BUY":
		if price <= pos.StopLoss {
			return models.CloseReasonStopLoss
		}
		if pos.TakeProfit > 0 && price >= pos.TakeProfit {
			return models.CloseReasonTakeProfit
		}
	case "SELL":
		if price >= pos.StopLoss {
			return models.CloseReasonStopLoss
		}
		if pos.TakeProfit > 0 && price <= pos.TakeProfit {
			return models.CloseReasonTakeProfit
		}
	}
	return ""
}

// ExitFillPrice — цена исполнения выхода в paper/virtual, когда уровень пересечён
// непрерывно (внутри бара по OHLC). SL/TP — по уровню; EOD и прочее — по marketPrice.
// Если цена уже была за стопом в момент проверки (бар открылся за ним, тик перескочил
// уровень) — это ExitFillPriceAt.
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

// ExitFillPriceAt — цена выхода, когда tradable — реальная цена рынка в момент
// проверки (open бара, тик), а не экстремум OHLC. Стоп исполняется по худшей из
// двух: бар, открывшийся за стопом, исполняет его по open, а не по уровню. Раньше
// всегда брался уровень — на синтетическом мартингале это давало плюс до издержек
// (docs/analysis/0006). Тейк — по уровню: лучше уровня не обещаем.
func ExitFillPriceAt(pos *State, reason string, tradable float64) float64 {
	px := ExitFillPrice(pos, reason, tradable)
	if pos == nil || reason != models.CloseReasonStopLoss {
		return px
	}
	if pos.Direction == "SELL" {
		return math.Max(px, tradable)
	}
	return math.Min(px, tradable)
}

// SameBarExitAfterFill — после limit-fill внутри бара (entry ≠ close): пробит ли SL по OHLC.
// Для входа по close (fade/MF и т.п.) возвращает "" — wick до закрытия бара ещё не «в позиции».
//
// Тейк в баре фила не засчитывается никогда. Лимит стоит позади цены, поэтому бар,
// в котором он исполнился, обычно сначала дошёл до тейка и лишь потом откатился к
// уровню: по OHLC тейк почти всегда задет ДО входа. Засчитывание такого тейка
// завышало baseline с +0.298R до +0.458R (docs/analysis/0005). Стоп в баре фила
// по-прежнему засчитывается: чтобы дойти до стопа, цена должна пройти уровень входа.
func SameBarExitAfterFill(pos *State, candle models.Candle) string {
	if pos == nil || pos.EntryPrice <= 0 {
		return ""
	}
	// Вход по цене закрытия бара — same-bar OHLC до entry не применяем.
	if pos.EntryAtBarClose {
		return ""
	}
	switch pos.Direction {
	case "BUY":
		if pos.StopLoss > 0 && candle.Low <= pos.StopLoss {
			return models.CloseReasonStopLoss
		}
	case "SELL":
		if pos.StopLoss > 0 && candle.High >= pos.StopLoss {
			return models.CloseReasonStopLoss
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

// IntrabarPath — синтетический путь цены внутри свечи для проверки SL/TP:
// Open → adverse → favorable → Close (для BUY: O, L, H, C). При касании стопа и тейка
// в одном баре засчитывается стоп — сознательно в худшую сторону. Стоп внутри бара
// фиксирован: трейл пересчитывается по закрытому бару (TrailPrice), иначе порядок
// экстремумов решал бы за модель (docs/analysis/0006).
func IntrabarPath(candle models.Candle, direction string) []float64 {
	switch direction {
	case "BUY":
		return []float64{candle.Open, candle.Low, candle.High, candle.Close}
	case "SELL":
		return []float64{candle.Open, candle.High, candle.Low, candle.Close}
	}
	return []float64{candle.Close}
}

// TrailPrice — цена, по которой трейл пересчитывается на закрытии бара: лучший
// экстремум бара в сторону позиции. Новый стоп действует со следующего бара (в live —
// со следующего тика); если он оказался за текущей ценой, выход по ExitFillPriceAt.
func TrailPrice(candle models.Candle, direction string) float64 {
	if direction == "SELL" {
		return candle.Low
	}
	return candle.High
}
