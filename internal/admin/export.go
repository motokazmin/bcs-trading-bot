package admin

import (
	"context"
	_ "embed"
	"fmt"
	"strings"
	"time"

	"bcs-trading-bot/pkg/interfaces"
	"bcs-trading-bot/pkg/models"
)

//go:embed prompts/strategy_summary.md
var strategySummaryPromptTemplate string

//go:embed prompts/strategy_detailed.md
var strategyDetailedPromptTemplate string

const exportVersion = "2.0"

// ExportMode — вариант выгрузки для ИИ.
type ExportMode string

const (
	ExportModeSummary  ExportMode = "summary"
	ExportModeDetailed ExportMode = "detailed"
)

func ParseExportMode(s string) (ExportMode, error) {
	switch strings.TrimSpace(s) {
	case "", "summary":
		return ExportModeSummary, nil
	case "detailed":
		return ExportModeDetailed, nil
	default:
		return "", fmt.Errorf("неизвестный mode: %q (ожидается summary или detailed)", s)
	}
}

func (m ExportMode) DataFilename() string {
	switch m {
	case ExportModeDetailed:
		return "data-trades.json"
	default:
		return "data-summary.json"
	}
}

func (m ExportMode) Label() string {
	switch m {
	case ExportModeDetailed:
		return "Подробный (с разбором сделок)"
	default:
		return "Краткий (по метрикам)"
	}
}

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

type exportBase struct {
	experiments []models.ExperimentReport
	comparison  []models.BreakdownRow
	dateRange   models.DateRange
	totalTrades int
}

func (s *ExportService) buildBase(ctx context.Context, f models.TradeFilter) (exportBase, error) {
	dateRange, err := s.reader.GetDateRange(ctx, f)
	if err != nil {
		return exportBase{}, err
	}

	experimentIDs, err := s.reader.ListExperimentIDs(ctx, f)
	if err != nil {
		return exportBase{}, err
	}

	comparison, err := s.reader.GetBreakdown(ctx, f, "experiment_id")
	if err != nil {
		return exportBase{}, err
	}

	var experiments []models.ExperimentReport
	totalTrades := 0

	for _, expID := range experimentIDs {
		expFilter := f
		expFilter.ExperimentID = expID

		report, err := s.buildExperimentReport(ctx, expFilter)
		if err != nil {
			return exportBase{}, fmt.Errorf("experiment %s: %w", expID, err)
		}
		experiments = append(experiments, report)
		totalTrades += report.Summary.TradeCount
	}

	return exportBase{
		experiments: experiments,
		comparison:  comparison,
		dateRange:   dateRange,
		totalTrades: totalTrades,
	}, nil
}

// BuildExportData — только данные для вложения (без промпта).
func (s *ExportService) BuildExportData(ctx context.Context, f models.TradeFilter, mode ExportMode) (models.ExportData, error) {
	base, err := s.buildBase(ctx, f)
	if err != nil {
		return models.ExportData{}, err
	}

	experiments := base.experiments
	if mode == ExportModeSummary {
		experiments = stripTrades(experiments)
	}

	return models.ExportData{
		ExportVersion:   exportVersion,
		ExportMode:      string(mode),
		ExportedAt:      time.Now().UTC(),
		Filters:         f,
		DateRange:       base.dateRange,
		StrategyContext: defaultStrategyContext(),
		Experiments:     experiments,
		Comparison:      base.comparison,
	}, nil
}

// BuildPrompt — только инструкции для вставки в чат (без данных).
func (s *ExportService) BuildPrompt(ctx context.Context, f models.TradeFilter, mode ExportMode) (string, error) {
	base, err := s.buildBase(ctx, f)
	if err != nil {
		return "", err
	}
	return renderPrompt(mode, base.dateRange, base.totalTrades), nil
}

func stripTrades(experiments []models.ExperimentReport) []models.ExperimentReport {
	out := make([]models.ExperimentReport, len(experiments))
	for i, exp := range experiments {
		exp.Trades = nil
		out[i] = exp
	}
	return out
}

func renderPrompt(mode ExportMode, dateRange models.DateRange, totalTrades int) string {
	tmpl := strategySummaryPromptTemplate
	if mode == ExportModeDetailed {
		tmpl = strategyDetailedPromptTemplate
	}

	replacements := map[string]string{
		"{{EXPORT_VERSION}}": exportVersion,
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

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}
