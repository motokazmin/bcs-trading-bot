package eval

import (
	"fmt"
	"math"
	"time"

	core "bcs-trading-bot/internal/optimizer/core"
	"bcs-trading-bot/pkg/logx"
)

// WindowResult — метрики одного окна.
type WindowResult struct {
	Metrics      core.Metrics                         `json:"metrics"`
	BySide       map[string]TradeSideStats            `json:"by_side,omitempty"`
	ByTickerSide map[string]map[string]TradeSideStats `json:"by_ticker_side,omitempty"`
}

// TradeSideStats — статистика успешности сделок по направлению.
type TradeSideStats struct {
	Trades   int     `json:"trades"`
	Wins     int     `json:"wins"`
	WinRate  float64 `json:"win_rate"`
	TotalPnL float64 `json:"total_pnl"`
}

// TrialResult — результат одного trial.
type TrialResult struct {
	Index   int               `json:"index"`
	Params  core.ParameterSet `json:"params"`
	Score   float64           `json:"score"`
	Windows []WindowResult    `json:"windows"`
}

// RunResult — полный результат оптимизации.
type RunResult struct {
	Timestamp time.Time     `json:"timestamp"`
	Trials    []TrialResult `json:"trials"`
	Best      TrialResult   `json:"best"`
}

func optimizerProgressInterval(total int) int {
	switch {
	case total <= 5:
		return 1
	case total <= 25:
		return 5
	default:
		interval := total / 20
		if interval < 10 {
			return 10
		}
		return interval
	}
}

func logOptimizerProgress(done, total int, bestScore float64, started time.Time) {
	elapsed := time.Since(started)
	pct := done * 100 / total
	msg := fmt.Sprintf("optimizer: %d/%d (%d%%) best_score=%s elapsed=%s",
		done, total, pct, formatOptimizerScore(bestScore), elapsed.Round(time.Second))
	if done > 0 && done < total {
		eta := time.Duration(int64(elapsed) * int64(total-done) / int64(done))
		msg += fmt.Sprintf(" eta=%s", eta.Round(time.Second))
	}
	logx.Info("%s", msg)
}

func formatOptimizerScore(s float64) string {
	switch {
	case math.IsInf(s, -1):
		return "-Inf"
	case math.IsInf(s, 1):
		return "+Inf"
	case math.IsNaN(s):
		return "NaN"
	case math.Abs(s) >= 100:
		return fmt.Sprintf("%.0f", s)
	default:
		return fmt.Sprintf("%.4f", s)
	}
}

func totalWindowPnL(t TrialResult) float64 {
	var sum float64
	for _, w := range t.Windows {
		sum += w.Metrics.TotalPnL
	}
	return sum
}
