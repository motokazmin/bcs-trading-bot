package core_test

import (
	"math"
	"math/rand"
	"testing"

	"bcs-trading-bot/internal/optimizer/core"
)

// step держит параметр в минутах на сетке таймфрейма: orbMinutes 12…22 на M15 даёт
// всего два разных диапазона, и оптимизатор выбирал бы между копиями наугад.
// Все значения сетки должны встречаться, и ничего вне её.
func TestStepДержитСетку(t *testing.T) {
	space := &core.SearchSpace{
		Parameters: map[string]core.ParamBounds{
			"orbMinutes": {Type: core.ParamInt, Min: 15, Max: 45, Step: 15},
		},
	}
	rng := rand.New(rand.NewSource(7))
	seen := map[float64]int{}
	for i := 0; i < 600; i++ {
		v := space.Sample(rng)["orbMinutes"]
		if math.Mod(v-15, 15) != 0 || v < 15 || v > 45 {
			t.Fatalf("orbMinutes=%v вне сетки 15/30/45", v)
		}
		seen[v]++
	}
	for _, want := range []float64{15, 30, 45} {
		if seen[want] == 0 {
			t.Fatalf("значение %v ни разу не выбрано: %v", want, seen)
		}
	}
}
