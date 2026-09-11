package core_test

import (
	"math/rand"
	"testing"

	"bcs-trading-bot/internal/optimizer/core"
)

// Относительный параметр (of) существует ради одного: trailActivationR обязан быть
// НИЖЕ rewardRatio. Иначе позиция закрывается по тейку раньше, чем включается трейл,
// параметр мёртв, градиента по нему нет — и оптимизатор берёт его случайным.
// Так уже вышло дважды, см. docs/analysis/0003-reoptimization-protocol.md
func TestОтносительныйПараметрВсегдаНижеБазы(t *testing.T) {
	space := &core.SearchSpace{
		Parameters: map[string]core.ParamBounds{
			"rewardRatio":      {Type: core.ParamFloat, Min: 1.0, Max: 2.5},
			"trailActivationR": {Type: core.ParamFloat, Min: 0.3, Max: 0.95, Of: "rewardRatio"},
		},
	}
	rng := rand.New(rand.NewSource(42))
	for i := 0; i < 2000; i++ {
		p := space.Sample(rng)
		rr, act := p["rewardRatio"], p["trailActivationR"]
		if act >= rr {
			t.Fatalf("trial %d: активация %.4f >= тейк %.4f — трейлинг мёртв", i, act, rr)
		}
		if act < 0.3*rr-1e-9 || act > 0.95*rr+1e-9 {
			t.Fatalf("trial %d: активация %.4f вне [0.3,0.95]×%.4f", i, act, rr)
		}
	}
}

// База может быть константой из fixed, а не поиском.
func TestОтносительныйПараметрОтКонстанты(t *testing.T) {
	space := &core.SearchSpace{
		Parameters: map[string]core.ParamBounds{
			"trailActivationR": {Type: core.ParamFloat, Min: 0.5, Max: 0.5, Of: "rewardRatio"},
		},
		Fixed: map[string]float64{"rewardRatio": 2.0},
	}
	p := space.Sample(rand.New(rand.NewSource(1)))
	if got := p["trailActivationR"]; got != 1.0 {
		t.Fatalf("0.5 × 2.0: got %v, want 1.0", got)
	}
}

// Явная константа сильнее вычисленной доли — иначе fixed перестал бы работать.
func TestFixedСильнееОтносительного(t *testing.T) {
	space := &core.SearchSpace{
		Parameters: map[string]core.ParamBounds{
			"trailActivationR": {Type: core.ParamFloat, Min: 0.5, Max: 0.5, Of: "rewardRatio"},
		},
		Fixed: map[string]float64{"rewardRatio": 2.0, "trailActivationR": 1.7},
	}
	p := space.Sample(rand.New(rand.NewSource(1)))
	if got := p["trailActivationR"]; got != 1.7 {
		t.Fatalf("fixed: got %v, want 1.7", got)
	}
}
