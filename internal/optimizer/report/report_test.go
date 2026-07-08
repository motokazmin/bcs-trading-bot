package report

import (
	"math"
	"strings"
	"testing"
	"time"

	"bcs-trading-bot/internal/optimizer/eval"
)

func TestFormatOptimizerScore(t *testing.T) {
	if formatOptimizerScore(-1.5) != "-1.5000" {
		t.Fatal("finite score")
	}
	if formatOptimizerScore(math.Inf(-1)) != "-Inf" {
		t.Fatal("neg inf")
	}
}

func TestMarshalRunResultJSONWithInf(t *testing.T) {
	result := &eval.RunResult{
		Timestamp: time.Now(),
		Trials: []eval.TrialResult{{
			Index: 0,
			Score: math.Inf(-1),
		}},
		Best: eval.TrialResult{Index: 0, Score: math.Inf(-1)},
	}
	data, err := marshalRunResultJSON(result)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"score": null`) {
		t.Fatalf("expected null scores in JSON: %s", data)
	}
}
