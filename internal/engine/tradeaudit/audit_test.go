package tradeaudit_test

import (
	"testing"
	"time"

	"bcs-trading-bot/internal/engine/tradeaudit"
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

// Rejects — гейт входа: error режет сделку, warn/info пропускают.
func TestResultRejectsOnlyOnError(t *testing.T) {
	// Реальная сделка id=17 из trades.db: фил 278.61 записан на 1.95R выше
	// закрытия сигнального бара, вход сразу за стопом.
	bad := tradeaudit.ValidateOpen(tradeaudit.OpenInput{
		Direction:  "BUY",
		EntryPrice: 278.61,
		StopLoss:   278.22,
		TakeProfit: 279.25,
		RDistance:  0.389,
		BarClose:   277.85,
		LastPrice:  277.85,
	})
	if !bad.Rejects() {
		t.Fatalf("вход за стопом должен отклоняться, got %+v", bad)
	}

	clean := tradeaudit.ValidateOpen(tradeaudit.OpenInput{
		Direction:  "BUY",
		EntryPrice: 100,
		StopLoss:   99.5,
		TakeProfit: 101.3,
		RDistance:  0.5,
		BarClose:   100,
		LastPrice:  100,
	})
	if clean.Rejects() {
		t.Fatalf("чистый вход не должен отклоняться, got %+v", clean)
	}

	// Умеренный дрейф лимита — warn, сделка проходит.
	warn := tradeaudit.ValidateOpen(tradeaudit.OpenInput{
		Direction:  "BUY",
		EntryPrice: 100,
		StopLoss:   99.5,
		TakeProfit: 101.3,
		RDistance:  0.5,
		BarClose:   99.6,
		LastPrice:  99.6,
	})
	if warn.Severity != tradeaudit.SeverityWarn {
		t.Fatalf("severity: got %q, want warn (%+v)", warn.Severity, warn)
	}
	if warn.Rejects() {
		t.Fatal("warn не должен резать вход")
	}
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

// Дрейф лимита в НАШУ сторону — не повод резать вход. После фикса фила это
// штатная ситуация: лимит поймал откат, бар закрылся выше входа (для BUY).
func TestValidateOpenFavorableDriftIsNotError(t *testing.T) {
	favorable := tradeaudit.ValidateOpen(tradeaudit.OpenInput{
		Direction:  "BUY",
		EntryPrice: 100,
		StopLoss:   99.5,
		TakeProfit: 101.3,
		RDistance:  0.5,
		BarClose:   101.2, // +2.4R в нашу сторону
		LastPrice:  101.2,
	})
	if favorable.Rejects() {
		t.Fatalf("благоприятный дрейф не должен резать вход, got %+v", favorable)
	}

	// Тот же размер дрейфа, но против позиции — по-прежнему ошибка.
	adverse := tradeaudit.ValidateOpen(tradeaudit.OpenInput{
		Direction:  "BUY",
		EntryPrice: 100,
		StopLoss:   99.5,
		TakeProfit: 101.3,
		RDistance:  0.5,
		BarClose:   98.8, // −2.4R против нас, ниже стопа
		LastPrice:  98.8,
	})
	if !adverse.Rejects() {
		t.Fatalf("дрейф против позиции должен резать вход, got %+v", adverse)
	}
}
