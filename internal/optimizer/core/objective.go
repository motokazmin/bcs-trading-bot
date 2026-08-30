package core

import (
	"math"
	"sort"

	"bcs-trading-bot/internal/engine/costs"
	"bcs-trading-bot/internal/models"
)

// Metrics — агрегированные метрики backtest.
type Metrics struct {
	TotalPnL    float64 `json:"total_pnl"`
	Sharpe      float64 `json:"sharpe"`
	MaxDrawdown float64 `json:"max_drawdown"`
	Calmar      float64 `json:"calmar"`
	NumTrades   int     `json:"num_trades"`
	WinRate     float64 `json:"win_rate"`
}

// Score возвращает целевую метрику; -Inf при недостаточном числе сделок.
// Для прибыльных прогонов — Calmar; для убыточных — TotalPnL (руб.), чтобы различать конфиги.
func Score(m Metrics, minTrades int) float64 {
	if m.NumTrades < minTrades {
		return math.Inf(-1)
	}
	if m.MaxDrawdown <= 0 {
		return m.TotalPnL
	}
	if m.TotalPnL <= 0 {
		return m.TotalPnL
	}
	return m.Calmar
}

// ComputeMetrics считает метрики по net PnL сделок (комиссия уже вычтена).
func ComputeMetrics(netPnLs []float64, returns []float64) Metrics {
	if len(netPnLs) == 0 {
		return Metrics{}
	}

	total := 0.0
	wins := 0
	for _, pnl := range netPnLs {
		total += pnl
		if pnl > 0 {
			wins++
		}
	}

	equity := 0.0
	peak := 0.0
	maxDD := 0.0
	for _, pnl := range netPnLs {
		equity += pnl
		if equity > peak {
			peak = equity
		}
		dd := peak - equity
		if dd > maxDD {
			maxDD = dd
		}
	}

	sharpe := sharpeRatio(returns)
	calmar := 0.0
	if maxDD > 0 {
		calmar = total / maxDD
	}

	return Metrics{
		TotalPnL:    total,
		Sharpe:      sharpe,
		MaxDrawdown: maxDD,
		Calmar:      calmar,
		NumTrades:   len(netPnLs),
		WinRate:     float64(wins) / float64(len(netPnLs)),
	}
}

func sharpeRatio(returns []float64) float64 {
	if len(returns) < 2 {
		return 0
	}
	mean := 0.0
	for _, r := range returns {
		mean += r
	}
	mean /= float64(len(returns))

	variance := 0.0
	for _, r := range returns {
		d := r - mean
		variance += d * d
	}
	variance /= float64(len(returns) - 1)
	if variance <= 0 {
		return 0
	}
	return mean / math.Sqrt(variance)
}

// MedianFloat возвращает медиану среза; -Inf/NaN пропускаются.
func MedianFloat(values []float64) float64 {
	filtered := filterFiniteScores(values)
	if len(filtered) == 0 {
		return math.Inf(-1)
	}
	sort.Float64s(filtered)
	mid := len(filtered) / 2
	if len(filtered)%2 == 0 {
		return (filtered[mid-1] + filtered[mid]) / 2
	}
	return filtered[mid]
}

// MeanFloat возвращает среднее среза; -Inf/NaN пропускаются.
func MeanFloat(values []float64) float64 {
	filtered := filterFiniteScores(values)
	if len(filtered) == 0 {
		return math.Inf(-1)
	}
	sum := 0.0
	for _, v := range filtered {
		sum += v
	}
	return sum / float64(len(filtered))
}

func filterFiniteScores(values []float64) []float64 {
	out := make([]float64, 0, len(values))
	for _, v := range values {
		if math.IsInf(v, 0) || math.IsNaN(v) {
			continue
		}
		out = append(out, v)
	}
	return out
}

// NetPnLFromTrade вычитает комиссию round-trip из gross PnL сделки.
func NetPnLFromTrade(trade models.ClosedTrade, cfg costs.Config, classCode string) float64 {
	cc := trade.ClassCode
	if cc == "" {
		cc = classCode
	}
	step := trade.StepPriceValue
	if step <= 0 {
		step = 1
	}
	return costs.NetPnL(trade.GrossPnL, cfg, cc, trade.EntryPrice, trade.ExitPrice, trade.Quantity, step)
}

// NetPnLFromGross — legacy flat round-trip за quantity.
func NetPnLFromGross(grossPnL float64, quantity int, commissionPerLot float64) float64 {
	cfg := costs.Config{CommissionPerLot: commissionPerLot}
	return costs.NetPnL(grossPnL, cfg, costs.ClassCodeStocks, 0, 0, quantity, 1)
}
