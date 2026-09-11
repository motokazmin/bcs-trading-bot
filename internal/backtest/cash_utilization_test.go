package backtest

import "testing"

// Backtest обязан резервировать кэш так же, как live (selfmanaged.go): не весь
// баланс, а долю CashUtilizationPct. Разъедутся — backtest начнёт брать объём,
// которого живой бот не возьмёт, и портфельные числа станут недостижимы.
// См. docs/analysis/0002-live-config-drift-and-cash-sizing.md
func TestCapQuantityByCashРезервируетДолюКэша(t *testing.T) {
	// Баланс 200k, цена 100 => без доли влезло бы 2000 лотов, с 0.95 — 1900.
	cfg := RunnerConfig{StepPriceValue: 1}
	if got := cfg.capQuantityByCash(5000, 100, 200_000); got != 1900 {
		t.Fatalf("дефолт 0.95: got %d, want 1900 (весь баланс дал бы 2000)", got)
	}

	// Явное значение уважается.
	cfg.CashUtilizationPct = 0.5
	if got := cfg.capQuantityByCash(5000, 100, 200_000); got != 1000 {
		t.Fatalf("0.5: got %d, want 1000", got)
	}

	// Объём меньше доступного не трогаем.
	cfg.CashUtilizationPct = 0.95
	if got := cfg.capQuantityByCash(10, 100, 200_000); got != 10 {
		t.Fatalf("без капа: got %d, want 10", got)
	}
}

// Дефолт и границы — то же правило, что в selfmanaged.Config: 0 и мусор → 0.95.
func TestEffectiveCashUtilizationДефолт(t *testing.T) {
	for _, v := range []float64{0, -1, 1.5} {
		if got := (RunnerConfig{CashUtilizationPct: v}).effectiveCashUtilization(); got != 0.95 {
			t.Fatalf("CashUtilizationPct=%v: got %v, want 0.95", v, got)
		}
	}
	if got := (RunnerConfig{CashUtilizationPct: 0.8}).effectiveCashUtilization(); got != 0.8 {
		t.Fatalf("0.8: got %v, want 0.8", got)
	}
}
