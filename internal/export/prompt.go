package export

import (
	_ "embed"
	"fmt"
	"strings"
	"time"

	"bcs-trading-bot/pkg/models"
)

//go:embed prompts/strategy_summary.md
var strategySummaryPromptTemplate string

//go:embed prompts/strategy_detailed.md
var strategyDetailedPromptTemplate string

const Version = "2.3"

// Mode — вариант выгрузки для ИИ.
type Mode string

const (
	ModeSummary  Mode = "summary"
	ModeDetailed Mode = "detailed"
)

func (m Mode) DataFilename() string {
	switch m {
	case ModeDetailed:
		return "data-trades.json"
	default:
		return "data-summary.json"
	}
}

func (m Mode) PromptFilename() string {
	switch m {
	case ModeDetailed:
		return "prompt-detailed.md"
	default:
		return "prompt-summary.md"
	}
}

func (m Mode) Label() string {
	switch m {
	case ModeDetailed:
		return "Подробный (с разбором сделок)"
	default:
		return "Краткий (по метрикам)"
	}
}

// RenderPrompt формирует инструкции для чата (без данных).
func RenderPrompt(mode Mode, dateRange models.DateRange, totalTrades int) string {
	tmpl := strategySummaryPromptTemplate
	if mode == ModeDetailed {
		tmpl = strategyDetailedPromptTemplate
	}

	replacements := map[string]string{
		"{{EXPORT_VERSION}}": Version,
		"{{EXPORTED_AT}}":    time.Now().UTC().Format(time.RFC3339),
		"{{DATE_FROM}}":      orDash(dateRange.From),
		"{{DATE_TO}}":        orDash(dateRange.To),
		"{{TOTAL_TRADES}}":   fmt.Sprintf("%d", totalTrades),
	}

	out := tmpl
	for k, v := range replacements {
		out = strings.ReplaceAll(out, k, v)
	}
	return out
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}
