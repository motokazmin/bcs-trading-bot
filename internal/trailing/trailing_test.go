package trailing_test

import (
	"testing"

	"bcs-trading-bot/internal/position"
	"bcs-trading-bot/internal/trailing"
)

func TestApplyTrailingStopBuy(t *testing.T) {
	cfg := trailing.DefaultConfig()
	cfg.CommissionPerLot = 5.0
	pos := &position.State{
		Direction:  "BUY",
		EntryPrice: 100,
		StopLoss:   95,
		RDistance:  5,
		TrailStage: 0,
		MFEPrice:   100,
	}

	trailing.Apply(pos, 104, cfg)
	if pos.TrailStage != 0 {
		t.Fatalf("stage before +1R: got %d, want 0", pos.TrailStage)
	}
	if pos.StopLoss != 95 {
		t.Fatalf("SL before +1R: got %.2f, want 95", pos.StopLoss)
	}

	trailing.Apply(pos, 105, cfg)
	position.UpdateMFE(pos, 105)
	if pos.TrailStage != 1 {
		t.Fatalf("stage at +1R: got %d, want 1", pos.TrailStage)
	}
	if pos.StopLoss != 105 {
		t.Fatalf("SL at +1R: got %.2f, want 105 (entry+commission)", pos.StopLoss)
	}

	prevSL := pos.StopLoss
	trailing.Apply(pos, 108, cfg)
	position.UpdateMFE(pos, 108)
	if pos.StopLoss != prevSL {
		t.Fatalf("SL must not move backward: got %.2f, want %.2f", pos.StopLoss, prevSL)
	}

	trailing.Apply(pos, 110, cfg)
	position.UpdateMFE(pos, 110)
	if pos.TrailStage != 2 {
		t.Fatalf("stage at +2R: got %d, want 2", pos.TrailStage)
	}
	if pos.StopLoss != 105 {
		t.Fatalf("SL at +2R: got %.2f, want 105 (entry+1R)", pos.StopLoss)
	}

	trailing.Apply(pos, 95, cfg)
	if pos.StopLoss != 105 {
		t.Fatalf("SL must not rollback on price drop: got %.2f, want 105", pos.StopLoss)
	}
	if pos.TrailStage != 2 {
		t.Fatalf("stage must not rollback: got %d, want 2", pos.TrailStage)
	}
}

func TestApplyTrailingStopVariantCBuy(t *testing.T) {
	cfg := trailing.DefaultConfig()
	cfg.CommissionPerLot = 5.0
	pos := &position.State{
		Direction:  "BUY",
		EntryPrice: 100,
		StopLoss:   105,
		RDistance:  5,
		TrailStage: 2,
		MFEPrice:   110,
	}

	position.UpdateMFE(pos, 115)
	trailing.Apply(pos, 115, cfg)
	if pos.StopLoss != 110 {
		t.Fatalf("SL at +3R: got %.2f, want 110 (MFE-1R)", pos.StopLoss)
	}

	trailing.Apply(pos, 108, cfg)
	if pos.StopLoss != 110 {
		t.Fatalf("SL must not rollback on pullback: got %.2f, want 110", pos.StopLoss)
	}
}

func TestApplyTrailingStopSell(t *testing.T) {
	cfg := trailing.DefaultConfig()
	cfg.CommissionPerLot = 5.0
	pos := &position.State{
		Direction:  "SELL",
		EntryPrice: 100,
		StopLoss:   105,
		RDistance:  5,
		TrailStage: 0,
		MFEPrice:   100,
	}

	trailing.Apply(pos, 95, cfg)
	position.UpdateMFE(pos, 95)
	if pos.TrailStage != 1 {
		t.Fatalf("stage at +1R: got %d, want 1", pos.TrailStage)
	}
	if pos.StopLoss != 95 {
		t.Fatalf("SL at +1R: got %.2f, want 95", pos.StopLoss)
	}

	trailing.Apply(pos, 90, cfg)
	position.UpdateMFE(pos, 90)
	if pos.TrailStage != 2 {
		t.Fatalf("stage at +2R: got %d, want 2", pos.TrailStage)
	}
	if pos.StopLoss != 95 {
		t.Fatalf("SL at +2R: got %.2f, want 95 (entry-1R)", pos.StopLoss)
	}

	trailing.Apply(pos, 110, cfg)
	if pos.StopLoss != 95 {
		t.Fatalf("SL must not rollback: got %.2f, want 95", pos.StopLoss)
	}
}

func TestApplyTrailingStopFuturesStepPrice(t *testing.T) {
	cfg := trailing.DefaultConfig()
	cfg.CommissionPerLot = 5.0
	cfg.StepPriceValue = 1.2
	pos := &position.State{
		Direction:  "BUY",
		EntryPrice: 100,
		StopLoss:   99,
		RDistance:  1,
		TrailStage: 0,
		MFEPrice:   100,
	}

	trailing.Apply(pos, 101, cfg)
	position.UpdateMFE(pos, 101)
	if pos.TrailStage != 1 {
		t.Fatalf("stage at +1R: got %d, want 1", pos.TrailStage)
	}
	wantSL := 100 + 5.0/1.2
	if pos.StopLoss != wantSL {
		t.Fatalf("breakeven SL: got %.4f, want %.4f", pos.StopLoss, wantSL)
	}
}
