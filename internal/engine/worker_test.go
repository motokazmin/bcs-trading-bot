package engine

import "testing"

func TestApplyTrailingStopBuy(t *testing.T) {
	pos := &openPosition{
		direction:  "BUY",
		entryPrice: 100,
		stopLoss:   95,
		rDistance:  5,
		trailStage: 0,
	}

	applyTrailingStop(pos, 104, 1.0)
	if pos.trailStage != 0 {
		t.Fatalf("stage before +1R: got %d, want 0", pos.trailStage)
	}
	if pos.stopLoss != 95 {
		t.Fatalf("SL before +1R: got %.2f, want 95", pos.stopLoss)
	}

	applyTrailingStop(pos, 105, 1.0)
	if pos.trailStage != 1 {
		t.Fatalf("stage at +1R: got %d, want 1", pos.trailStage)
	}
	if pos.stopLoss != 105 {
		t.Fatalf("SL at +1R: got %.2f, want 105 (entry+commission)", pos.stopLoss)
	}

	prevSL := pos.stopLoss
	applyTrailingStop(pos, 108, 1.0)
	if pos.stopLoss != prevSL {
		t.Fatalf("SL must not move backward: got %.2f, want %.2f", pos.stopLoss, prevSL)
	}

	applyTrailingStop(pos, 110, 1.0)
	if pos.trailStage != 2 {
		t.Fatalf("stage at +2R: got %d, want 2", pos.trailStage)
	}
	if pos.stopLoss != 105 {
		t.Fatalf("SL at +2R: got %.2f, want 105 (entry+1R)", pos.stopLoss)
	}

	applyTrailingStop(pos, 95, 1.0)
	if pos.stopLoss != 105 {
		t.Fatalf("SL must not rollback on price drop: got %.2f, want 105", pos.stopLoss)
	}
	if pos.trailStage != 2 {
		t.Fatalf("stage must not rollback: got %d, want 2", pos.trailStage)
	}
}

func TestApplyTrailingStopSell(t *testing.T) {
	pos := &openPosition{
		direction:  "SELL",
		entryPrice: 100,
		stopLoss:   105,
		rDistance:  5,
		trailStage: 0,
	}

	applyTrailingStop(pos, 95, 1.0)
	if pos.trailStage != 1 {
		t.Fatalf("stage at +1R: got %d, want 1", pos.trailStage)
	}
	if pos.stopLoss != 95 {
		t.Fatalf("SL at +1R: got %.2f, want 95", pos.stopLoss)
	}

	applyTrailingStop(pos, 90, 1.0)
	if pos.trailStage != 2 {
		t.Fatalf("stage at +2R: got %d, want 2", pos.trailStage)
	}
	if pos.stopLoss != 95 {
		t.Fatalf("SL at +2R: got %.2f, want 95 (entry-1R)", pos.stopLoss)
	}

	applyTrailingStop(pos, 110, 1.0)
	if pos.stopLoss != 95 {
		t.Fatalf("SL must not rollback: got %.2f, want 95", pos.stopLoss)
	}
}

func TestApplyTrailingStopFuturesStepPrice(t *testing.T) {
	pos := &openPosition{
		direction:  "BUY",
		entryPrice: 100,
		stopLoss:   99,
		rDistance:  1,
		trailStage: 0,
	}

	applyTrailingStop(pos, 101, 1.2)
	if pos.trailStage != 1 {
		t.Fatalf("stage at +1R: got %d, want 1", pos.trailStage)
	}
	wantSL := 100 + virtualCommissionPerLot/1.2
	if pos.stopLoss != wantSL {
		t.Fatalf("breakeven SL: got %.4f, want %.4f", pos.stopLoss, wantSL)
	}
}
