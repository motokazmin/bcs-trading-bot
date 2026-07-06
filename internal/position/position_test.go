package position_test

import (
	"testing"

	"bcs-trading-bot/internal/position"
)

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
