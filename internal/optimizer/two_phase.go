package optimizer

import (
	"context"
	"fmt"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"bcs-trading-bot/pkg/logx"
	"bcs-trading-bot/pkg/models"
)

// TwoPhaseConfig — двухфазный поиск: random search на lean universe, финал на полном.
type TwoPhaseConfig struct {
	OptimizationConfig
	Phase1Tickers []string
	Phase2Top     int
}

// RunTwoPhaseOptimization выполняет фазу 1 на подмножестве тикеров и фазу 2 на полном universe.
func RunTwoPhaseOptimization(
	ctx context.Context,
	settings RunSettings,
	space *SearchSpace,
	candleData map[string][]models.Candle,
	windows []Window,
	cfg TwoPhaseConfig,
) *RunResult {
	phase2Top := cfg.Phase2Top
	if phase2Top <= 0 {
		phase2Top = 20
	}

	phase1Tickers := intersectTickers(settings.Tickers, cfg.Phase1Tickers)
	if len(phase1Tickers) == 0 {
		logx.Warn("two-phase: phase1 tickers пуст — fallback на полный список")
		phase1Tickers = append([]string(nil), settings.Tickers...)
	}

	phase1Settings := settings
	phase1Settings.Tickers = phase1Tickers

	logx.Info("two-phase: фаза 1 — %d trials на %s", cfg.Trials, strings.Join(phase1Tickers, ","))
	eval1 := NewEvaluator(phase1Settings, space, candleData)
	eval1.PrecomputeWindowSlices(windows)
	phase1 := RunOptimizationWithConfig(ctx, eval1, space, windows, cfg.OptimizationConfig)

	if sameTickerSets(phase1Tickers, settings.Tickers) {
		logx.Info("two-phase: phase1 tickers = полный universe, фаза 2 пропущена")
		return phase1
	}

	topN := phase2Top
	if topN > len(phase1.Trials) {
		topN = len(phase1.Trials)
	}
	if topN == 0 {
		return phase1
	}

	candidates := append([]TrialResult(nil), phase1.Trials[:topN]...)
	logx.Info("two-phase: фаза 2 — top %d конфигов на %s", topN, strings.Join(settings.Tickers, ","))

	eval2 := NewEvaluator(settings, space, candleData)
	eval2.PrecomputeWindowSlices(windows)

	parallel := cfg.Parallelism
	if parallel <= 0 {
		parallel = runtime.NumCPU()
	}
	if parallel > len(candidates) {
		parallel = len(candidates)
	}

	results := make([]TrialResult, len(candidates))
	var wg sync.WaitGroup
	sem := make(chan struct{}, parallel)

	for i, cand := range candidates {
		wg.Add(1)
		sem <- struct{}{}
		go func(index int, params ParameterSet) {
			defer wg.Done()
			defer func() { <-sem }()
			if ctx.Err() != nil {
				return
			}
			results[index] = eval2.evaluateTrial(ctx, index, params, cfg.MinTrades)
		}(i, cand.Params)
	}
	wg.Wait()

	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	best := TrialResult{}
	if len(results) > 0 {
		best = results[0]
	}

	logx.Info("two-phase: фаза 2 лучший score=%s (фаза 1 была %s)",
		formatOptimizerScore(best.Score), formatOptimizerScore(phase1.Best.Score))

	return &RunResult{
		Timestamp: time.Now(),
		Trials:    results,
		Best:      best,
	}
}

func intersectTickers(full, subset []string) []string {
	set := make(map[string]struct{}, len(full))
	for _, t := range full {
		set[t] = struct{}{}
	}
	out := make([]string, 0, len(subset))
	for _, t := range subset {
		if _, ok := set[t]; ok {
			out = append(out, t)
		}
	}
	return out
}

func sameTickerSets(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	set := make(map[string]struct{}, len(a))
	for _, t := range a {
		set[t] = struct{}{}
	}
	for _, t := range b {
		if _, ok := set[t]; !ok {
			return false
		}
	}
	return true
}

// ResolvePhase1Tickers возвращает тикеры для фазы 1: override, lean из tickers-config или default.
func ResolvePhase1Tickers(override string, lean []string, full []string) ([]string, error) {
	if override != "" {
		return normalizeSymbols(strings.Split(override, ",")), nil
	}
	if len(lean) > 0 {
		return intersectTickers(full, lean), nil
	}
	defaultLean := []string{"SBER", "ROSN", "NVTK"}
	out := intersectTickers(full, defaultLean)
	if len(out) == 0 {
		return nil, fmt.Errorf("two-phase: нет пересечения lean tickers с загруженным universe")
	}
	return out, nil
}
