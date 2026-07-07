package export

import (
	"sort"

	"bcs-trading-bot/pkg/models"
)

// tradePnL возвращает PnL сделки в рублях (уже net или gross — как передано в GrossPnL).
func tradePnL(t models.ClosedTrade) float64 {
	return t.GrossPnL
}

func isWinner(t models.ClosedTrade) bool {
	if t.IsWinner && tradePnL(t) > 0 {
		return true
	}
	return tradePnL(t) > 0
}

// BuildExperimentReport собирает отчёт по сделкам (in-memory, без БД).
func BuildExperimentReport(experimentID, stopMode string, trades []models.ClosedTrade) models.ExperimentReport {
	sorted := append([]models.ClosedTrade(nil), trades...)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].ClosedAt.Before(sorted[j].ClosedAt)
	})

	return models.ExperimentReport{
		ExperimentID:  experimentID,
		StopMode:      stopMode,
		Summary:       buildSummary(sorted),
		ByTicker:      buildBreakdown(sorted, func(t models.ClosedTrade) string { return t.Ticker }),
		ByCloseReason: buildBreakdown(sorted, func(t models.ClosedTrade) string { return t.CloseReason }),
		DailyPnL:      buildDailyPnL(sorted),
		EquityCurve:   buildEquityCurve(sorted),
		Trades:        sorted,
	}
}

func buildSummary(trades []models.ClosedTrade) models.TradeSummary {
	if len(trades) == 0 {
		return models.TradeSummary{}
	}

	var (
		total, avgR       float64
		wins, losses      int
		grossWins         float64
		grossLosses       float64
		avgWin, avgLoss   float64
		winSum, lossSum   float64
		best, worst       float64
		holdSec           float64
	)

	best = tradePnL(trades[0])
	worst = best

	for _, t := range trades {
		pnl := tradePnL(t)
		total += pnl
		avgR += t.PnLR
		holdSec += float64(t.HoldSeconds)

		if pnl > best {
			best = pnl
		}
		if pnl < worst {
			worst = pnl
		}

		if isWinner(t) {
			wins++
			grossWins += pnl
			winSum += pnl
		} else if pnl < 0 {
			losses++
			grossLosses += pnl
			lossSum += pnl
		}
	}

	n := len(trades)
	summary := models.TradeSummary{
		TradeCount:    n,
		WinCount:      wins,
		LossCount:     losses,
		TotalPnL:      total,
		AvgPnL:        total / float64(n),
		AvgPnLR:       avgR / float64(n),
		AvgHoldSec:    holdSec / float64(n),
		BestTradePnL:  best,
		WorstTradePnL: worst,
	}
	if n > 0 {
		summary.WinRate = float64(wins) / float64(n) * 100
	}
	if grossLosses < 0 {
		summary.ProfitFactor = grossWins / (-grossLosses)
	}
	if wins > 0 {
		avgWin = winSum / float64(wins)
	}
	if losses > 0 {
		avgLoss = lossSum / float64(losses)
	}
	winRate := float64(wins) / float64(n)
	lossRate := float64(losses) / float64(n)
	summary.Expectancy = winRate*avgWin + lossRate*avgLoss
	summary.ExpectancyR = summary.AvgPnLR

	return summary
}

func buildBreakdown(trades []models.ClosedTrade, keyFn func(models.ClosedTrade) string) []models.BreakdownRow {
	type acc struct {
		row         models.BreakdownRow
		wins        int
		grossWins   float64
		grossLosses float64
	}
	byKey := make(map[string]*acc)

	for _, t := range trades {
		key := keyFn(t)
		if key == "" {
			key = "—"
		}
		a, ok := byKey[key]
		if !ok {
			a = &acc{row: models.BreakdownRow{Key: key}}
			byKey[key] = a
		}
		pnl := tradePnL(t)
		a.row.TradeCount++
		a.row.TotalPnL += pnl
		a.row.AvgPnLR += t.PnLR
		if isWinner(t) {
			a.wins++
			a.grossWins += pnl
		} else if pnl < 0 {
			a.grossLosses += pnl
		}
	}

	out := make([]models.BreakdownRow, 0, len(byKey))
	for _, a := range byKey {
		row := a.row
		if row.TradeCount > 0 {
			row.AvgPnLR /= float64(row.TradeCount)
			row.WinRate = float64(a.wins) / float64(row.TradeCount) * 100
		}
		if a.grossLosses < 0 {
			row.ProfitFactor = a.grossWins / (-a.grossLosses)
		}
		out = append(out, row)
	}

	sort.Slice(out, func(i, j int) bool {
		return out[i].TotalPnL > out[j].TotalPnL
	})
	return out
}

func buildDailyPnL(trades []models.ClosedTrade) []models.DailyPnLRow {
	type acc struct {
		row  models.DailyPnLRow
		wins int
	}
	byDate := make(map[string]*acc)

	for _, t := range trades {
		date := t.TradingDate
		if date == "" {
			date = t.ClosedAt.UTC().Format("2006-01-02")
		}
		a, ok := byDate[date]
		if !ok {
			a = &acc{row: models.DailyPnLRow{TradingDate: date}}
			byDate[date] = a
		}
		a.row.TradeCount++
		a.row.TotalPnL += tradePnL(t)
		if isWinner(t) {
			a.wins++
		}
	}

	out := make([]models.DailyPnLRow, 0, len(byDate))
	for _, a := range byDate {
		row := a.row
		if row.TradeCount > 0 {
			row.WinRate = float64(a.wins) / float64(row.TradeCount) * 100
		}
		out = append(out, row)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].TradingDate < out[j].TradingDate
	})
	return out
}

func buildEquityCurve(trades []models.ClosedTrade) []models.EquityPoint {
	out := make([]models.EquityPoint, 0, len(trades))
	var cumulative float64
	for _, t := range trades {
		cumulative += tradePnL(t)
		out = append(out, models.EquityPoint{
			ClosedAt:      t.ClosedAt,
			CumulativePnL: cumulative,
		})
	}
	return out
}

// DateRangeFromTrades возвращает min/max trading_date.
func DateRangeFromTrades(trades []models.ClosedTrade) models.DateRange {
	if len(trades) == 0 {
		return models.DateRange{}
	}
	from := trades[0].TradingDate
	to := from
	for _, t := range trades[1:] {
		d := t.TradingDate
		if d == "" {
			d = t.ClosedAt.UTC().Format("2006-01-02")
		}
		if from == "" || d < from {
			from = d
		}
		if d > to {
			to = d
		}
	}
	return models.DateRange{From: from, To: to}
}

// StripTrades убирает сделки из отчётов (summary mode).
func StripTrades(experiments []models.ExperimentReport) []models.ExperimentReport {
	out := make([]models.ExperimentReport, len(experiments))
	for i, exp := range experiments {
		exp.Trades = nil
		out[i] = exp
	}
	return out
}
