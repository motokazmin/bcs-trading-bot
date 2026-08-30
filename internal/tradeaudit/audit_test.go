package tradeaudit_test

import (
	"testing"
	"time"

	"bcs-trading-bot/internal/tradeaudit"
	"bcs-trading-bot/internal/models"
)

func TestValidateOpenTATNLimitStale(t *testing.T) {
	r := tradeaudit.ValidateOpen(tradeaudit.OpenInput{
		Direction:  "SELL",
		EntryPrice: 484.7,
		StopLoss:   485.84,
		TakeProfit: 482.83,
		RDistance:  1.14,
		BarClose:   491.6,
		LastPrice:  491.7,
		RewardRatio: 1.64,
	})
	if r.Severity != tradeaudit.SeverityError {
		t.Fatalf("severity: got %q, want error", r.Severity)
	}
	assertHas(t, r, tradeaudit.CodeLimitVsClose)
	assertHas(t, r, tradeaudit.CodeEntryPastStop)
}

func TestValidateOpenCloseEntryClean(t *testing.T) {
	r := tradeaudit.ValidateOpen(tradeaudit.OpenInput{
		Direction:   "SELL",
		EntryPrice:  8.834,
		StopLoss:    8.865,
		TakeProfit:  8.794,
		RDistance:   0.031,
		BarClose:    8.834,
		LastPrice:   8.834,
		RewardRatio: 1.27,
	})
	if !r.Empty() {
		t.Fatalf("close-entry should be clean, got %+v", r)
	}
}

func TestValidateCloseSLFillDrift(t *testing.T) {
	r := tradeaudit.ValidateClose(tradeaudit.CloseInput{
		Direction:     "SELL",
		EntryPrice:    8.834,
		ExitPrice:     8.884,
		FinalStopLoss: 8.865,
		RDistance:     0.0314,
		CloseReason:   models.CloseReasonStopLoss,
		PnLR:          -1.6,
		HoldSeconds:   561,
	})
	assertHas(t, r, tradeaudit.CodeSLFillDrift)
	if r.Severity != tradeaudit.SeverityWarn {
		t.Fatalf("severity: got %q, want warn", r.Severity)
	}
}

func TestValidateCloseSLAtLevelClean(t *testing.T) {
	r := tradeaudit.ValidateClose(tradeaudit.CloseInput{
		Direction:     "SELL",
		EntryPrice:    484.7,
		ExitPrice:     485.84,
		FinalStopLoss: 485.84,
		RDistance:     1.14,
		CloseReason:   models.CloseReasonStopLoss,
		PnLR:          -1.0,
		HoldSeconds:   1,
		SameBarExit:   true,
		BarDuration:   5 * time.Minute,
	})
	assertHas(t, r, tradeaudit.CodeSameBarSL)
	assertNotHas(t, r, tradeaudit.CodeSLFillDrift)
}

func TestValidateCloseTrailDead(t *testing.T) {
	r := tradeaudit.ValidateClose(tradeaudit.CloseInput{
		Direction:       "BUY",
		EntryPrice:      100,
		ExitPrice:       101.27,
		TakeProfit:      101.27,
		RDistance:       1,
		CloseReason:     models.CloseReasonTakeProfit,
		TrailStage:      0,
		TrailActivation: 2.0,
		MFEinR:          1.27,
	})
	assertHas(t, r, tradeaudit.CodeTrailDead)
}

func assertHas(t *testing.T, r tradeaudit.Result, code string) {
	t.Helper()
	for _, c := range r.Codes {
		if c == code {
			return
		}
	}
	t.Fatalf("missing code %s in %v", code, r.Codes)
}

func assertNotHas(t *testing.T, r tradeaudit.Result, code string) {
	t.Helper()
	for _, c := range r.Codes {
		if c == code {
			t.Fatalf("unexpected code %s in %v", code, r.Codes)
		}
	}
}
