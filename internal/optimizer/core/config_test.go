package core

import (
	"math/rand"
	"testing"
)

func TestSampleMergesFixedIntoParams(t *testing.T) {
	space := &SearchSpace{
		Strategy: "momentum_filtered",
		Parameters: map[string]ParamBounds{
			"lookback": {Type: ParamInt, Min: 10, Max: 10},
		},
		Fixed: map[string]float64{
			"longOnly":            1,
			"riskPerTradePercent": 0.5,
		},
	}
	params := space.Sample(rand.New(rand.NewSource(1)))
	if params["lookback"] != 10 {
		t.Fatalf("lookback=%v, want 10", params["lookback"])
	}
	if params["longOnly"] != 1 {
		t.Fatalf("longOnly=%v, want 1 (from fixed)", params["longOnly"])
	}
	if params["riskPerTradePercent"] != 0.5 {
		t.Fatalf("riskPerTradePercent=%v, want 0.5", params["riskPerTradePercent"])
	}
}

func TestApplyFixedOverridesSampledKey(t *testing.T) {
	space := &SearchSpace{
		Strategy: "momentum_filtered",
		Parameters: map[string]ParamBounds{
			"longOnly": {Type: ParamInt, Min: 0, Max: 0},
		},
		Fixed: map[string]float64{
			"longOnly": 1,
		},
	}
	params := space.Sample(rand.New(rand.NewSource(42)))
	if params["longOnly"] != 1 {
		t.Fatalf("fixed must win over sampled: got %v", params["longOnly"])
	}
}
