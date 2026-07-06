package optimizer

import (
	"math"
	"math/rand"
)

// Searcher — интерфейс алгоритма поиска гиперпараметров.
// TODO: реализовать TPE/Bayesian searcher.
type Searcher interface {
	Suggest() ParameterSet
	Report(params ParameterSet, score float64)
	Best() (ParameterSet, float64)
}

// RandomSearcher — baseline random search.
type RandomSearcher struct {
	space     *SearchSpace
	trials    int
	rng       *rand.Rand
	current   int
	best      ParameterSet
	bestScore float64
}

func NewRandomSearcher(space *SearchSpace, trials int, seed int64) *RandomSearcher {
	return &RandomSearcher{
		space:     space,
		trials:    trials,
		rng:       rand.New(rand.NewSource(seed)),
		bestScore: math.Inf(-1),
	}
}

func (s *RandomSearcher) Suggest() ParameterSet {
	return s.space.Sample(s.rng)
}

func (s *RandomSearcher) Report(params ParameterSet, score float64) {
	s.current++
	if score > s.bestScore {
		s.bestScore = score
		s.best = params
	}
}

func (s *RandomSearcher) Best() (ParameterSet, float64) {
	return s.best, s.bestScore
}

func (s *RandomSearcher) Done() bool {
	return s.current >= s.trials
}
