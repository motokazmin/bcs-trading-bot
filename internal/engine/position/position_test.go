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
		Direction:  "BUY",
		EntryPrice: 100,
		StopLoss:   95,
		TakeProfit: 110,
	}
	candle := models.Candle{Open: 102, High: 103, Low: 94, Close: 100}
	if got := position.SameBarExitAfterFill(pos, candle); got != "" {
		t.Fatalf("close entry must not same-bar exit on pre-entry wick, got %q", got)
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
