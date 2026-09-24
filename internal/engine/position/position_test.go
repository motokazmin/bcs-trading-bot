package position_test

import (
	"testing"

	"bcs-trading-bot/internal/engine/position"
	"bcs-trading-bot/internal/models"
)

func TestExitFillPriceStopLossUsesLevel(t *testing.T) {
	pos := &position.State{Direction: "SELL", StopLoss: 485.84, TakeProfit: 482.8}
	got := position.ExitFillPrice(pos, models.CloseReasonStopLoss, 491.7)
	if got != 485.84 {
		t.Fatalf("SL fill: got %.2f, want 485.84 (not adverse tick)", got)
	}
}

// TakeProfit == 0 означает «тейк отключён». Без явной проверки условие
// price >= 0 для лонга истинно всегда и позиция закрывалась бы сразу как TP.
func TestCheckExitNoTakeProfit(t *testing.T) {
	long := &position.State{Direction: "BUY", EntryPrice: 100, StopLoss: 99, TakeProfit: 0}
	if got := position.CheckExit(long, 100.5); got != "" {
		t.Fatalf("BUY без тейка: got %q, want \"\"", got)
	}
	if got := position.CheckExit(long, 98.9); got != models.CloseReasonStopLoss {
		t.Fatalf("BUY без тейка, цена под стопом: got %q, want STOP_LOSS", got)
	}

	short := &position.State{Direction: "SELL", EntryPrice: 100, StopLoss: 101, TakeProfit: 0}
	if got := position.CheckExit(short, 99.5); got != "" {
		t.Fatalf("SELL без тейка: got %q, want \"\"", got)
	}
	if got := position.CheckExit(short, 101.1); got != models.CloseReasonStopLoss {
		t.Fatalf("SELL без тейка, цена над стопом: got %q, want STOP_LOSS", got)
	}
}

func TestSameBarExitAfterFillShortTATN(t *testing.T) {
	// Paper bug scenario: limit short 484.7, stop 485.84, bar high 500 / close 491.6.
	pos := &position.State{
		Direction:  "SELL",
		EntryPrice: 484.7,
		StopLoss:   485.84,
		TakeProfit: 482.83,
	}
	candle := models.Candle{Open: 482.9, High: 500, Low: 482.9, Close: 491.6}
	reason := position.SameBarExitAfterFill(pos, candle)
	if reason != models.CloseReasonStopLoss {
		t.Fatalf("reason: got %q, want STOP_LOSS", reason)
	}
	exit := position.ExitFillPrice(pos, reason, candle.Close)
	if exit != 485.84 {
		t.Fatalf("exit: got %.2f, want stop 485.84", exit)
	}
}

func TestSameBarExitAfterFillNoHit(t *testing.T) {
	pos := &position.State{
		Direction:  "BUY",
		EntryPrice: 100,
		StopLoss:   95,
		TakeProfit: 110,
	}
	candle := models.Candle{Open: 100, High: 105, Low: 98, Close: 103}
	if got := position.SameBarExitAfterFill(pos, candle); got != "" {
		t.Fatalf("unexpected exit %q", got)
	}
}

func TestSameBarExitSkipsCloseBasedEntry(t *testing.T) {
	// Fade/MF: вход по close; Low до close не должен дать мгновенный SL.
	pos := &position.State{
		Direction:       "BUY",
		EntryPrice:      100,
		StopLoss:        95,
		TakeProfit:      110,
		EntryAtBarClose: true,
	}
	candle := models.Candle{Open: 102, High: 103, Low: 94, Close: 100}
	if got := position.SameBarExitAfterFill(pos, candle); got != "" {
		t.Fatalf("close entry must not same-bar exit on pre-entry wick, got %q", got)
	}
}

// Признак «вход по close» должен быть явным полем, а не выводиться сравнением
// EntryPrice с candle.Close: проскальзывание сдвигает цену фила, и по сравнению
// вход по закрытию перестаёт распознаваться — стратегия получает ложный -1R
// по хвосту свечи, который случился ДО входа.
func TestSameBarExitCloseEntrySurvivesSlippage(t *testing.T) {
	candle := models.Candle{Open: 102, High: 103, Low: 94, Close: 100}
	pos := &position.State{
		Direction:       "BUY",
		EntryPrice:      100.01, // фил с проскальзыванием: уже != candle.Close
		StopLoss:        95,
		TakeProfit:      110,
		EntryAtBarClose: true,
	}
	if got := position.SameBarExitAfterFill(pos, candle); got != "" {
		t.Fatalf("вход по close со скольжением не должен давать same-bar выход, got %q", got)
	}

	// А настоящий limit-fill внутри бара по-прежнему проверяется по OHLC.
	limitPos := &position.State{
		Direction:       "BUY",
		EntryPrice:      100.01,
		StopLoss:        95,
		TakeProfit:      110,
		EntryAtBarClose: false,
	}
	if got := position.SameBarExitAfterFill(limitPos, candle); got != models.CloseReasonStopLoss {
		t.Fatalf("limit-fill: got %q, want STOP_LOSS", got)
	}
}

func TestCalcMFEinR(t *testing.T) {
	pos := &position.State{
		Direction:  "BUY",
		EntryPrice: 100,
		RDistance:  5,
		MFEPrice:   114,
	}
	if got := position.CalcMFEinR(pos); got != 2.8 {
		t.Fatalf("MFEinR: got %.2f, want 2.8", got)
	}
}

func TestCalcMAEinR(t *testing.T) {
	pos := &position.State{
		Direction:  "BUY",
		EntryPrice: 100,
		RDistance:  5,
		MAEPrice:   92,
	}
	if got := position.CalcMAEinR(pos); got != 1.6 {
		t.Fatalf("MAEinR: got %.2f, want 1.6", got)
	}
}

func TestUpdateMAE(t *testing.T) {
	pos := &position.State{Direction: "BUY", EntryPrice: 100, MAEPrice: 100}
	position.UpdateMAE(pos, 99)
	position.UpdateMAE(pos, 97)
	if pos.MAEPrice != 97 {
		t.Fatalf("maePrice: got %.2f, want 97", pos.MAEPrice)
	}
}
