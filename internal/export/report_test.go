package export_test

import (
	"testing"
	"time"

	"bcs-trading-bot/internal/costs"
	"bcs-trading-bot/internal/export"
	"bcs-trading-bot/pkg/models"
)

func TestBuildExperimentReportSummary(t *testing.T) {
	now := time.Date(2024, 6, 1, 10, 0, 0, 0, time.UTC)
	trades := []models.ClosedTrade{
		{Ticker: "SBER", GrossPnL: 100, PnLR: 1, CloseReason: models.CloseReasonTakeProfit, ClosedAt: now, IsWinner: true},
		{Ticker: "GAZP", GrossPnL: -50, PnLR: -0.5, CloseReason: models.CloseReasonStopLoss, ClosedAt: now.Add(time.Hour)},
	}

	report := export.BuildExperimentReport("test-exp", "atr", trades)

	if report.Summary.TradeCount != 2 {
		t.Fatalf("trade count: %d", report.Summary.TradeCount)
	}
	if report.Summary.TotalPnL != 50 {
		t.Fatalf("total pnl: %.2f", report.Summary.TotalPnL)
	}
	if report.Summary.ExpectancyR != 0.25 {
		t.Fatalf("expectancy_r: %.2f", report.Summary.ExpectancyR)
	}
	if len(report.ByTicker) != 2 {
		t.Fatalf("by ticker: %d", len(report.ByTicker))
	}
	if len(report.EquityCurve) != 2 {
		t.Fatalf("equity: %d", len(report.EquityCurve))
	}
}

func TestApplyNetPnL(t *testing.T) {
	trades := []models.ClosedTrade{{
		GrossPnL: 100, Quantity: 10, RDistance: 1, StepPriceValue: 1,
	}}
	out := export.ApplyNetPnL(trades, costs.Config{CommissionPerLot: 0.10}, costs.ClassCodeStocks)
	if out[0].GrossPnL != 99 {
		t.Fatalf("net: %.2f", out[0].GrossPnL)
	}
}

func TestRenderPromptContainsMeta(t *testing.T) {
	prompt := export.RenderPrompt(export.ModeSummary, models.DateRange{From: "2024-01-01", To: "2024-06-01"}, 42)
	if prompt == "" {
		t.Fatal("empty prompt")
	}
	if !contains(prompt, "data-summary.json") {
		t.Fatal("missing filename")
	}
	if !contains(prompt, "42") {
		t.Fatal("missing trade count")
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
