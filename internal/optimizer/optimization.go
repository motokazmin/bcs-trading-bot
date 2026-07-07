package optimizer

import (
	"context"
	"math"
	"math/rand"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"bcs-trading-bot/pkg/logx"
)

// OptimizationConfig — параметры walk-forward random search.
type OptimizationConfig struct {
	Trials      int
	MinTrades   int
	Seed        int64
	Parallelism int // 0 = runtime.NumCPU()
}

// RunOptimization выполняет walk-forward random search.
func RunOptimization(ctx context.Context, evaluator *Evaluator, space *SearchSpace, windows []Window, trials, minTrades int, seed int64) *RunResult {
	return RunOptimizationWithConfig(ctx, evaluator, space, windows, OptimizationConfig{
		Trials:    trials,
		MinTrades: minTrades,
		Seed:      seed,
	})
}

// RunOptimizationWithConfig выполняет walk-forward random search с настройкой параллелизма.
func RunOptimizationWithConfig(ctx context.Context, evaluator *Evaluator, space *SearchSpace, windows []Window, cfg OptimizationConfig) *RunResult {
	if len(evaluator.windowSlices) == 0 {
		evaluator.PrecomputeWindowSlices(windows)
	}

	trials := cfg.Trials
	if trials <= 0 {
		trials = 1
	}
	parallel := cfg.Parallelism
	if parallel <= 0 {
		parallel = runtime.NumCPU()
	}
	if parallel > trials {
		parallel = trials
	}

	trialParams := make([]ParameterSet, trials)
	rng := rand.New(rand.NewSource(cfg.Seed))
	for i := range trialParams {
		trialParams[i] = space.Sample(rng)
	}

	results := make([]TrialResult, trials)
	interval := optimizerProgressInterval(trials)
	started := time.Now()

	logx.Info("optimizer: старт %d trials, %d walk-forward окон, parallel=%d", trials, len(windows), parallel)

	var (
		doneCount  atomic.Int32
		bestMu     sync.Mutex
		bestScore  = math.Inf(-1)
		progressMu sync.Mutex
	)

	var wg sync.WaitGroup
	sem := make(chan struct{}, parallel)

	for i := 0; i < trials; i++ {
		if ctx.Err() != nil {
			break
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(index int, params ParameterSet) {
			defer wg.Done()
			defer func() { <-sem }()

			if ctx.Err() != nil {
				return
			}

			result := evaluator.evaluateTrial(ctx, index, params, cfg.MinTrades)
			results[index] = result

			bestMu.Lock()
			if result.Score > bestScore {
				bestScore = result.Score
			}
			currentBest := bestScore
			bestMu.Unlock()

			done := int(doneCount.Add(1))
			if done == trials || done%interval == 0 {
				progressMu.Lock()
				logOptimizerProgress(done, trials, currentBest, started)
				progressMu.Unlock()
			}
		}(i, trialParams[i])
	}

	wg.Wait()

	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	completed := 0
	for _, r := range results {
		if r.Params != nil {
			completed++
		}
	}

	best := TrialResult{}
	if completed > 0 {
		best = results[0]
	}

	elapsed := time.Since(started).Round(time.Second)
	logx.Info("optimizer: готово %d/%d trials за %s, best score=%s",
		completed, trials, elapsed, formatOptimizerScore(best.Score))
	if completed > 0 {
		bestPnL := totalWindowPnL(best)
		switch {
		case math.IsInf(best.Score, -1):
			logx.Warn("optimizer: ни один trial не набрал min-trades — уменьшите -min-trades или сузьте search-space (breakoutThreshold > 0.05 почти не даёт сделок)")
		case best.Score < 0:
			logx.Warn("optimizer: walk-forward убыточен (score=%s, PnL=%.0f руб.) — см. JSON",
				formatOptimizerScore(best.Score), bestPnL)
		case bestPnL <= 0:
			// Score — медиана Calmar по окнам: у части окон может быть неплохой
			// Calmar (мало сделок, маленькая просадка), а сумма PnL по всем окнам
			// всё равно в минусе. Положительный score НЕ означает, что конфигурация
			// заработала деньги за весь период.
			logx.Warn("optimizer: best score=%s положителен (медиана Calmar по окнам), но суммарный PnL по всем окнам = %.0f руб. — конфигурация НЕ прибыльна в деньгах, см. JSON по окнам",
				formatOptimizerScore(best.Score), bestPnL)
		}
	}

	return &RunResult{
		Timestamp: time.Now(),
		Trials:    results,
		Best:      best,
	}
}
