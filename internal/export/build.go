package export

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"bcs-trading-bot/internal/costs"
	"bcs-trading-bot/pkg/models"
)

// PackageOptions — параметры сборки ExportData.
type PackageOptions struct {
	Mode            Mode
	Source          string
	Optimizer       *models.OptimizerExportInfo
	StrategyContext models.StrategyContext
	Filters         models.TradeFilter
	Experiments     []models.ExperimentReport
	Comparison      []models.BreakdownRow
	DateRange       models.DateRange
}

// BuildData собирает ExportData.
func BuildData(opts PackageOptions) models.ExportData {
	experiments := opts.Experiments
	if opts.Mode == ModeSummary {
		experiments = StripTrades(experiments)
	}

	source := opts.Source
	if source == "" {
		source = "live"
	}

	return models.ExportData{
		ExportVersion:   Version,
		ExportMode:      string(opts.Mode),
		ExportedAt:      time.Now().UTC(),
		Source:          source,
		Filters:         opts.Filters,
		DateRange:       opts.DateRange,
		StrategyContext: opts.StrategyContext,
		KeyMetrics:      BuildKeyMetrics(experiments),
		Experiments:     experiments,
		Comparison:      opts.Comparison,
		Optimizer:       opts.Optimizer,
	}
}

// WritePackage сохраняет JSON-данные и markdown-промпт в директорию.
func WritePackage(dir string, data models.ExportData, prompt string, mode Mode) (dataPath, promptPath string, err error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", "", err
	}

	dataPath = filepath.Join(dir, mode.DataFilename())
	raw, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return "", "", fmt.Errorf("marshal export: %w", err)
	}
	if err := os.WriteFile(dataPath, raw, 0o644); err != nil {
		return "", "", err
	}

	promptPath = filepath.Join(dir, mode.PromptFilename())
	if err := os.WriteFile(promptPath, []byte(prompt), 0o644); err != nil {
		return "", "", err
	}
	return dataPath, promptPath, nil
}

// ApplyNetPnL копирует сделки с net PnL в GrossPnL (для optimizer backtest).
func ApplyNetPnL(trades []models.ClosedTrade, commissionPerLot float64) []models.ClosedTrade {
	out := make([]models.ClosedTrade, len(trades))
	for i, t := range trades {
		out[i] = t
		net := costs.NetPnL(t.GrossPnL, t.Quantity, commissionPerLot)
		out[i].GrossPnL = net
		out[i].IsWinner = net > 0
		riskAmt := t.RDistance * float64(t.Quantity) * stepPriceValue(t)
		if riskAmt > 0 {
			out[i].PnLR = net / riskAmt
		}
	}
	return out
}

func stepPriceValue(t models.ClosedTrade) float64 {
	if t.StepPriceValue > 0 {
		return t.StepPriceValue
	}
	return 1.0
}

// BuildKeyMetrics агрегирует ключевые метрики по всем экспериментам.
func BuildKeyMetrics(experiments []models.ExperimentReport) models.KeyMetrics {
	var allTrades []models.ClosedTrade
	for _, exp := range experiments {
		if len(exp.Trades) > 0 {
			allTrades = append(allTrades, exp.Trades...)
		}
	}
	if len(allTrades) > 0 {
		s := buildSummary(allTrades)
		return models.KeyMetrics{
			TradeCount:    s.TradeCount,
			ExpectancyR:   s.ExpectancyR,
			ExpectancyRub: s.Expectancy,
			TotalPnLRub:   s.TotalPnL,
			WinRate:       s.WinRate,
			ProfitFactor:  s.ProfitFactor,
		}
	}

	var totalTrades, totalWins int
	var weightedR, totalPnL, pfWeighted float64
	for _, exp := range experiments {
		s := exp.Summary
		totalTrades += s.TradeCount
		weightedR += s.ExpectancyR * float64(s.TradeCount)
		totalPnL += s.TotalPnL
		totalWins += s.WinCount
		if s.TradeCount > 0 && s.ProfitFactor > 0 {
			pfWeighted += s.ProfitFactor * float64(s.TradeCount)
		}
	}

	km := models.KeyMetrics{
		TradeCount:  totalTrades,
		TotalPnLRub: totalPnL,
	}
	if totalTrades > 0 {
		km.ExpectancyR = weightedR / float64(totalTrades)
		km.ExpectancyRub = totalPnL / float64(totalTrades)
		km.WinRate = float64(totalWins) / float64(totalTrades) * 100
		if pfWeighted > 0 {
			km.ProfitFactor = pfWeighted / float64(totalTrades)
		}
	}
	return km
}
func TotalTrades(experiments []models.ExperimentReport) int {
	total := 0
	for _, exp := range experiments {
		total += exp.Summary.TradeCount
	}
	return total
}
