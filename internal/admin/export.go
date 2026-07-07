package admin

import (
	"context"
	"fmt"

	"bcs-trading-bot/internal/export"
	"bcs-trading-bot/pkg/interfaces"
	"bcs-trading-bot/pkg/models"
)

// ExportMode — вариант выгрузки для ИИ.
type ExportMode = export.Mode

const (
	ExportModeSummary  = export.ModeSummary
	ExportModeDetailed = export.ModeDetailed
)

func ParseExportMode(s string) (ExportMode, error) {
	switch s {
	case "", "summary":
		return ExportModeSummary, nil
	case "detailed":
		return ExportModeDetailed, nil
	default:
		return "", fmt.Errorf("неизвестный mode: %q (ожидается summary или detailed)", s)
	}
}

// ExportService собирает пакеты для веб-UI и ИИ-анализа.
type ExportService struct {
	reader interfaces.TradeReader
}

func NewExportService(reader interfaces.TradeReader) *ExportService {
	return &ExportService{reader: reader}
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

	return export.BuildData(export.PackageOptions{
		Mode:            mode,
		Source:          "live",
		StrategyContext: export.DefaultLiveStrategyContext(),
		Filters:         f,
		Experiments:     base.experiments,
		Comparison:      base.comparison,
		DateRange:       base.dateRange,
	}), nil
}

// BuildPrompt — только инструкции для вставки в чат (без данных).
func (s *ExportService) BuildPrompt(ctx context.Context, f models.TradeFilter, mode ExportMode) (string, error) {
	base, err := s.buildBase(ctx, f)
	if err != nil {
		return "", err
	}
	return export.RenderPrompt(mode, base.dateRange, base.totalTrades), nil
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
