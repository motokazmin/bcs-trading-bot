package admin

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"bcs-trading-bot/pkg/interfaces"
	"bcs-trading-bot/pkg/models"
)

//go:embed prompts/strategy_analysis.md
var strategyAnalysisPromptTemplate string

// ExportService собирает пакеты для веб-UI и ИИ-анализа.
type ExportService struct {
	reader interfaces.TradeReader
}

func NewExportService(reader interfaces.TradeReader) *ExportService {
	return &ExportService{reader: reader}
}

func defaultStrategyContext() models.StrategyContext {
	return models.StrategyContext{
		Name:           "Momentum Breakout (Неидеальный агент)",
		Philosophy:     "Рынок непредсказуем; управляем только риском. Прибыльность при win rate 30–40% за счёт R:R 1:3.",
		SignalLogic:    "Пробой high/low за lookback-1 свечей M5; вход лимитным ордером.",
		RiskReward:     "Stop-Loss и Take-Profit в соотношении 1:3 (1R риск, 3R цель).",
		RiskPerTrade:   "Размер лота из 0.5% депозита на тикер при срабатывании начального SL.",
		TrailingStop:   "+1R → безубыток; +2R → фиксация +1R; выход по SL/TP/EOD рыночным ордером.",
		CircuitBreaker: "2% дневного убытка на эксперимент → блокировка новых входов до следующего дня.",
		PnLNote:        "gross_pnl в рублях, комиссия не вычтена. pnl_r — результат в единицах R.",
		ExperimentNote: "Параллельные experiment_id — разные virtual-счета на одних рыночных данных (stop_mode: range | atr).",
	}
}

// BuildAIExport собирает полный пакет с данными и готовым промптом.
func (s *ExportService) BuildAIExport(ctx context.Context, f models.TradeFilter) (models.AIExportBundle, error) {
	dateRange, err := s.reader.GetDateRange(ctx, f)
	if err != nil {
		return models.AIExportBundle{}, err
	}

	experimentIDs, err := s.reader.ListExperimentIDs(ctx, f)
	if err != nil {
		return models.AIExportBundle{}, err
	}

	comparison, err := s.reader.GetBreakdown(ctx, f, "experiment_id")
	if err != nil {
		return models.AIExportBundle{}, err
	}

	var experiments []models.ExperimentReport
	totalTrades := 0

	for _, expID := range experimentIDs {
		expFilter := f
		expFilter.ExperimentID = expID

		report, err := s.buildExperimentReport(ctx, expFilter)
		if err != nil {
			return models.AIExportBundle{}, fmt.Errorf("experiment %s: %w", expID, err)
		}
		experiments = append(experiments, report)
		totalTrades += report.Summary.TradeCount
	}

	bundle := models.AIExportBundle{
		ExportVersion:   "1.0",
		ExportedAt:      time.Now().UTC(),
		Filters:         f,
		DateRange:       dateRange,
		StrategyContext: defaultStrategyContext(),
		Experiments:     experiments,
		Comparison:      comparison,
	}

	prompt, err := s.buildPrompt(bundle, totalTrades)
	if err != nil {
		return models.AIExportBundle{}, err
	}
	bundle.Prompt = prompt

	return bundle, nil
}

func (s *ExportService) buildExperimentReport(ctx context.Context, f models.TradeFilter) (models.ExperimentReport, error) {
	summary, err := s.reader.GetSummary(ctx, f)
	if err != nil {
		return models.ExperimentReport{}, err
	}
	byTicker, err := s.reader.GetBreakdown(ctx, f, "ticker")
	if err != nil {
		return models.ExperimentReport{}, err
	}
	byReason, err := s.reader.GetBreakdown(ctx, f, "close_reason")
	if err != nil {
		return models.ExperimentReport{}, err
	}
	daily, err := s.reader.GetDailyPnL(ctx, f)
	if err != nil {
		return models.ExperimentReport{}, err
	}
	equity, err := s.reader.GetEquityCurve(ctx, f)
	if err != nil {
		return models.ExperimentReport{}, err
	}

	trades, err := s.listAllTrades(ctx, f)
	if err != nil {
		return models.ExperimentReport{}, err
	}

	stopMode := ""
	if len(trades) > 0 {
		stopMode = trades[0].StopMode
	}

	return models.ExperimentReport{
		ExperimentID:  f.ExperimentID,
		StopMode:      stopMode,
		Summary:       summary,
		ByTicker:      byTicker,
		ByCloseReason: byReason,
		DailyPnL:      daily,
		EquityCurve:   equity,
		Trades:        trades,
	}, nil
}

func (s *ExportService) listAllTrades(ctx context.Context, f models.TradeFilter) ([]models.ClosedTrade, error) {
	const pageSize = 5000
	var all []models.ClosedTrade
	offset := 0
	for {
		page, err := s.reader.ListClosedTrades(ctx, f, pageSize, offset)
		if err != nil {
			return nil, err
		}
		all = append(all, page.Trades...)
		if len(all) >= page.Total || len(page.Trades) == 0 {
			break
		}
		offset += pageSize
	}
	return all, nil
}

func (s *ExportService) buildPrompt(bundle models.AIExportBundle, totalTrades int) (string, error) {
	ctxJSON, err := json.MarshalIndent(bundle.StrategyContext, "", "  ")
	if err != nil {
		return "", err
	}

	filtersJSON, err := json.Marshal(bundle.Filters)
	if err != nil {
		return "", err
	}

	// Для промпта — компактные данные без полного списка сделок (они в JSON-файле).
	compact := struct {
		ExportVersion   string                    `json:"export_version"`
		ExportedAt      time.Time                 `json:"exported_at"`
		DateRange       models.DateRange          `json:"date_range"`
		Filters         models.TradeFilter        `json:"filters"`
		StrategyContext models.StrategyContext    `json:"strategy_context"`
		Comparison      []models.BreakdownRow     `json:"comparison"`
		Experiments     []experimentCompact       `json:"experiments"`
	}{
		ExportVersion:   bundle.ExportVersion,
		ExportedAt:      bundle.ExportedAt,
		DateRange:       bundle.DateRange,
		Filters:         bundle.Filters,
		StrategyContext: bundle.StrategyContext,
		Comparison:      bundle.Comparison,
		Experiments:     make([]experimentCompact, len(bundle.Experiments)),
	}
	for i, exp := range bundle.Experiments {
		compact.Experiments[i] = experimentCompact{
			ExperimentID:  exp.ExperimentID,
			StopMode:      exp.StopMode,
			Summary:       exp.Summary,
			ByTicker:      exp.ByTicker,
			ByCloseReason: exp.ByCloseReason,
			DailyPnL:      exp.DailyPnL,
			EquityCurve:   exp.EquityCurve,
			TradeCount:    len(exp.Trades),
		}
	}

	dataJSON, err := json.MarshalIndent(compact, "", "  ")
	if err != nil {
		return "", err
	}

	replacements := map[string]string{
		"{{STRATEGY_CONTEXT}}":         string(ctxJSON),
		"{{EXPORT_VERSION}}":           bundle.ExportVersion,
		"{{EXPORTED_AT}}":              bundle.ExportedAt.Format(time.RFC3339),
		"{{DATE_FROM}}":                orDash(bundle.DateRange.From),
		"{{DATE_TO}}":                  orDash(bundle.DateRange.To),
		"{{FILTERS_JSON}}":             string(filtersJSON),
		"{{EXPERIMENT_COUNT}}":         fmt.Sprintf("%d", len(bundle.Experiments)),
		"{{TOTAL_TRADES}}":             fmt.Sprintf("%d", totalTrades),
		"{{EXPERIMENT_SUMMARY_TABLE}}": formatExperimentSummaryTable(bundle.Comparison),
		"{{DATA_JSON}}":                string(dataJSON),
	}

	out := strategyAnalysisPromptTemplate
	for k, v := range replacements {
		out = strings.ReplaceAll(out, k, v)
	}
	return out, nil
}

type experimentCompact struct {
	ExperimentID  string               `json:"experiment_id"`
	StopMode      string               `json:"stop_mode"`
	Summary       models.TradeSummary  `json:"summary"`
	ByTicker      []models.BreakdownRow `json:"by_ticker"`
	ByCloseReason []models.BreakdownRow `json:"by_close_reason"`
	DailyPnL      []models.DailyPnLRow `json:"daily_pnl"`
	EquityCurve   []models.EquityPoint `json:"equity_curve"`
	TradeCount    int                  `json:"trade_count"`
}

func formatExperimentSummaryTable(rows []models.BreakdownRow) string {
	if len(rows) == 0 {
		return "_Нет сделок в выборке._"
	}
	var b strings.Builder
	b.WriteString("| experiment_id | сделок | win% | total PnL | profit factor | avg R |\n")
	b.WriteString("|---|---:|---:|---:|---:|---:|\n")
	for _, r := range rows {
		fmt.Fprintf(&b, "| %s | %d | %.1f | %.2f | %.2f | %.2f |\n",
			r.Key, r.TradeCount, r.WinRate, r.TotalPnL, r.ProfitFactor, r.AvgPnLR)
	}
	return b.String()
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

// RenderPromptMarkdown оборачивает промпт в markdown-файл для скачивания.
func RenderPromptMarkdown(bundle models.AIExportBundle) string {
	var buf bytes.Buffer
	buf.WriteString("# Промпт для анализа стратегии BCS Trading Bot\n\n")
	buf.WriteString("Скопируйте блок ниже в ChatGPT / Claude / другой ИИ.\n\n")
	buf.WriteString("---\n\n")
	buf.WriteString(bundle.Prompt)
	buf.WriteString("\n\n---\n\n")
	buf.WriteString("> Для полного списка сделок приложите файл `export-ai.json` из админки.\n")
	return buf.String()
}
