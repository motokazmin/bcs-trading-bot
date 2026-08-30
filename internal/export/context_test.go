package export_test

import (
	"strings"
	"testing"

	"bcs-trading-bot/internal/export"
	"bcs-trading-bot/internal/models"
)

func TestDefaultLiveStrategyContextNoFixedRR13(t *testing.T) {
	ctx := export.DefaultLiveStrategyContext()
	blob := strings.Join([]string{
		ctx.Name, ctx.Philosophy, ctx.SignalLogic, ctx.RiskReward,
		ctx.TrailingStop, ctx.PnLNote, ctx.ExperimentNote,
	}, "\n")
	for _, myth := range []string{"за счёт R:R 1:3", "соотношении 1:3", "Stop-Loss и Take-Profit в соотношении 1:3"} {
		if strings.Contains(blob, myth) {
			t.Fatalf("default live context must not claim fixed R:R 1:3 (%q)", myth)
		}
	}
	if !strings.Contains(ctx.RiskReward, "1.2") {
		t.Fatal("expected champion reward_ratio range in risk_reward")
	}
}

func TestDetailedPromptGuardsAgainstRR13Myth(t *testing.T) {
	prompt := export.RenderPrompt(export.ModeDetailed, models.DateRange{From: "2026-07-01", To: "2026-08-01"}, 55)
	if strings.Contains(prompt, "за счёт R:R 1:3") {
		t.Fatal("detailed prompt must not hardcode R:R 1:3 philosophy")
	}
	if !strings.Contains(prompt, "Фактический R:R") {
		t.Fatal("detailed prompt should instruct computing R:R from trades")
	}
	if !strings.Contains(prompt, "котиров") {
		t.Fatal("detailed prompt should mention quote-based exits")
	}
	if strings.Contains(prompt, "хуже −1R") || strings.Contains(prompt, "хуже -1R") {
		t.Fatal("detailed prompt must not claim SL fills worse than stop level")
	}
	if !strings.Contains(prompt, "по уровню") {
		t.Fatal("detailed prompt should state SL/TP fill at level")
	}
}

func TestSummaryPromptGuardsAgainstRR13Myth(t *testing.T) {
	prompt := export.RenderPrompt(export.ModeSummary, models.DateRange{}, 10)
	if strings.Contains(prompt, "за счёт R:R 1:3") {
		t.Fatal("summary prompt must not hardcode R:R 1:3 philosophy")
	}
	if !strings.Contains(prompt, "1.2–1.8") && !strings.Contains(prompt, "1.2-1.8") {
		t.Fatal("summary prompt should mention champion reward_ratio range")
	}
	if !strings.Contains(prompt, "по уровню") {
		t.Fatal("summary prompt should mention SL/TP fill at level")
	}
}

func TestDefaultLiveStrategyContextFillAtLevel(t *testing.T) {
	ctx := export.DefaultLiveStrategyContext()
	if strings.Contains(ctx.PnLNote, "хуже") {
		t.Fatal("PnLNote must not claim adverse tick beyond stop")
	}
	if !strings.Contains(ctx.PnLNote, "по уровню") {
		t.Fatal("PnLNote should mention fill at stop/TP level")
	}
}
