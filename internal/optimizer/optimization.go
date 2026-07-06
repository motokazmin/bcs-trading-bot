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
		bestTest   = math.Inf(-1)
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
			if result.TestScore > bestTest {
				bestTest = result.TestScore
			}
			currentBest := bestTest
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
		return results[i].TestScore > results[j].TestScore
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
	logx.Info("optimizer: готово %d/%d trials за %s, best test_score=%s",
		completed, trials, elapsed, formatOptimizerScore(best.TestScore))
	if completed > 0 {
		bestPnL := totalTestPnL(best)
		switch {
		case math.IsInf(best.TestScore, -1):
			logx.Warn("optimizer: ни один trial не набрал min-trades на OOS — уменьшите -min-trades или сузьте search-space (breakoutThreshold > 0.05 почти не даёт сделок)")
		case best.TestScore < 0:
			logx.Warn("optimizer: OOS убыточен (score=%s, test PnL=%.0f руб.) — см. JSON",
				formatOptimizerScore(best.TestScore), bestPnL)
		case bestPnL <= 0:
			// TestScore — медиана Calmar по окнам: у части окон может быть неплохой
			// Calmar (мало сделок, маленькая просадка), а сумма PnL по всем окнам
			// всё равно в минусе. Положительный score НЕ означает, что конфигурация
			// заработала деньги за весь период — явно предупреждаем, иначе можно
			// принять "score > 0, предупреждения нет" за признак прибыльности.
			logx.Warn("optimizer: best test_score=%s положителен (медиана Calmar по окнам), но суммарный OOS PnL по всем окнам = %.0f руб. — конфигурация НЕ прибыльна в деньгах, см. JSON по окнам",
				formatOptimizerScore(best.TestScore), bestPnL)
		}
	}

	return &RunResult{
		Timestamp:  time.Now(),
		Trials:     results,
		BestByTest: best,
	}
}
