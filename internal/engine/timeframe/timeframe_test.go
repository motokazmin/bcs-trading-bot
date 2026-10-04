package timeframe

import (
	"testing"
	"time"

	"bcs-trading-bot/internal/models"
)

// Неизвестный таймфрейм: молчаливый fallback на M5 сдвигал бы конец бара, EOD и гейты входа.
func TestDurationRejectsUnknown(t *testing.T) {
	if d, err := Duration("m15 "); err != nil || d != 15*time.Minute {
		t.Fatalf("M15: %v, %v", d, err)
	}
	for _, tf := range []string{"M10", "", "weird"} {
		if _, err := Duration(tf); err == nil {
			t.Fatalf("%q: ожидалась ошибка", tf)
		}
	}
}

// M5 остаётся в базовой папке: на его файлы ссылаются sha256 в 0008 и .gitignore.
func TestHistoryDir(t *testing.T) {
	cases := map[string]string{"M5": "data/history", "": "data/history", "m15": "data/history-m15", "H1": "data/history-h1"}
	for tf, want := range cases {
		if got := HistoryDir("data/history", tf); got != want {
			t.Fatalf("%q: %q, want %q", tf, got, want)
		}
	}
}

// M5-история под видом M15 должна падать на загрузке, а не давать бэктест с барами по 15 мин.
func TestCheckGridCatchesForeignTimeframe(t *testing.T) {
	at := func(s string) models.Candle {
		ts, _ := time.Parse(time.RFC3339, s)
		return models.Candle{Timestamp: ts}
	}
	m15 := []models.Candle{at("2026-10-02T07:00:00Z"), at("2026-10-02T07:15:00Z")}
	if err := CheckGrid(m15, "M15"); err != nil {
		t.Fatalf("M15 на сетке: %v", err)
	}
	daily := []models.Candle{at("2026-10-01T21:00:00Z"), at("2026-10-02T21:00:00Z")}
	if err := CheckGrid(daily, "D"); err != nil {
		t.Fatalf("D на полуночи МСК: %v", err)
	}
	if err := CheckGrid([]models.Candle{at("2026-10-02T00:00:00Z")}, "D"); err == nil {
		t.Fatal("дневной бар не на полуночи МСК не пойман")
	}
	m5 := append(m15, at("2026-10-02T07:20:00Z"))
	if err := CheckGrid(m5, "M15"); err == nil {
		t.Fatal("M5-бар в M15-истории не пойман")
	}
}
