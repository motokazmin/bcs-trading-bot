package optimizer

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"bcs-trading-bot/internal/config"
	"bcs-trading-bot/internal/export"
	"bcs-trading-bot/pkg/logx"
	"bcs-trading-bot/pkg/models"
)

// AnalysisExportResult — пути к файлам экспорта для ИИ.
type AnalysisExportResult struct {
	Dir                string
	SummaryDataPath    string
	SummaryPromptPath  string
	DetailedDataPath   string
	DetailedPromptPath string
}

// WriteAnalysisExport сохраняет data-summary.json, data-trades.json и промпты.
func WriteAnalysisExport(expDir, expName string, cfg *config.Config, cfgPath string, trades []models.ClosedTrade, commission float64, meta *models.OptimizerExportInfo) (*AnalysisExportResult, error) {
	if len(trades) == 0 {
		return nil, fmt.Errorf("нет сделок для экспорта")
	}

	netTrades := export.ApplyNetPnL(trades, commission)
	normalizeTradesForExport(netTrades, expName, cfg)

	report := export.BuildExperimentReport(expName, cfg.Strategy.StopMode, netTrades)
	dateRange := export.DateRangeFromTrades(netTrades)
	if dateRange.From == "" && len(netTrades) > 0 {
		dateRange = models.DateRange{
			From: netTrades[0].ClosedAt.UTC().Format("2006-01-02"),
			To:   netTrades[len(netTrades)-1].ClosedAt.UTC().Format("2006-01-02"),
		}
	}

	if meta == nil {
		meta = &models.OptimizerExportInfo{}
	}
	meta.ExperimentName = expName
	if meta.BestConfig == "" {
		meta.BestConfig = filepath.Base(cfgPath)
	}
	if meta.CommissionPerLot <= 0 {
		meta.CommissionPerLot = commission
	}

	experiments := []models.ExperimentReport{report}
	comparison := []models.BreakdownRow{{
		Key:          expName,
		TradeCount:   report.Summary.TradeCount,
		WinRate:      report.Summary.WinRate,
		TotalPnL:     report.Summary.TotalPnL,
		AvgPnLR:      report.Summary.AvgPnLR,
		ProfitFactor: report.Summary.ProfitFactor,
	}}

	baseOpts := export.PackageOptions{
		Source:          "optimizer",
		StrategyContext: export.StrategyContextFromConfig(cfg),
		Filters: models.TradeFilter{
			ExperimentID: expName,
			TradingMode:  config.TradingModeVirtual,
			DateFrom:     dateRange.From,
			DateTo:       dateRange.To,
		},
		Experiments: experiments,
		Comparison:  comparison,
		DateRange:   dateRange,
		Optimizer:   meta,
	}

	exportDir := filepath.Join(expDir, "export")
	totalTrades := export.TotalTrades(experiments)

	summaryData := export.BuildData(copyPackageOpts(baseOpts, export.ModeSummary))
	summaryPrompt := export.RenderPrompt(export.ModeSummary, dateRange, totalTrades)
	summaryDataPath, summaryPromptPath, err := export.WritePackage(exportDir, summaryData, summaryPrompt, export.ModeSummary)
	if err != nil {
		return nil, err
	}

	detailedData := export.BuildData(copyPackageOpts(baseOpts, export.ModeDetailed))
	detailedPrompt := export.RenderPrompt(export.ModeDetailed, dateRange, totalTrades)
	detailedDataPath, detailedPromptPath, err := export.WritePackage(exportDir, detailedData, detailedPrompt, export.ModeDetailed)
	if err != nil {
		return nil, err
	}

	logx.Info("export: %s (%d trades) → %s", expName, totalTrades, exportDir)

	return &AnalysisExportResult{
		Dir:                exportDir,
		SummaryDataPath:    summaryDataPath,
		SummaryPromptPath:  summaryPromptPath,
		DetailedDataPath:   detailedDataPath,
		DetailedPromptPath: detailedPromptPath,
	}, nil
}

func copyPackageOpts(base export.PackageOptions, mode export.Mode) export.PackageOptions {
	base.Mode = mode
	return base
}

func loadOptimizerRunMeta(expDir string) *models.OptimizerExportInfo {
	matches, err := filepath.Glob(filepath.Join(expDir, "optimizer-run-*.json"))
	if err != nil || len(matches) == 0 {
		return nil
	}
	sort.Strings(matches)
	path := matches[len(matches)-1]

	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}

	var payload struct {
		Best struct {
			Score   float64 `json:"score"`
			Windows []struct {
				Metrics Metrics `json:"metrics"`
			} `json:"windows"`
		} `json:"best"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil
	}

	var totalPnL float64
	for _, w := range payload.Best.Windows {
		totalPnL += w.Metrics.TotalPnL
	}

	return &models.OptimizerExportInfo{
		BestConfig:       filepath.Base(path),
		WalkForwardScore: payload.Best.Score,
		TotalWindowPnL:   totalPnL,
	}
}

func normalizeTradesForExport(trades []models.ClosedTrade, expName string, cfg *config.Config) {
	for i := range trades {
		if trades[i].ExperimentID == "" {
			trades[i].ExperimentID = expName
		}
		if trades[i].StopMode == "" {
			trades[i].StopMode = cfg.Strategy.StopMode
		}
		if trades[i].ClassCode == "" {
			trades[i].ClassCode = cfg.ClassCode
		}
		if trades[i].CandleTimeframe == "" {
			trades[i].CandleTimeframe = cfg.CandleTimeFrame
		}
		if trades[i].Lookback == 0 {
			trades[i].Lookback = cfg.Strategy.Lookback
		}
		if trades[i].TradingDate == "" {
			trades[i].TradingDate = trades[i].ClosedAt.UTC().Format("2006-01-02")
		}
		if trades[i].HoldSeconds == 0 && trades[i].ClosedAt.After(trades[i].OpenedAt) {
			trades[i].HoldSeconds = int(trades[i].ClosedAt.Sub(trades[i].OpenedAt).Seconds())
		}
		trades[i].TradingMode = config.TradingModeVirtual
	}
}

func sortTradesByClose(trades []models.ClosedTrade) {
	sort.Slice(trades, func(i, j int) bool {
		return trades[i].ClosedAt.Before(trades[j].ClosedAt)
	})
}
