package optimizer

import (
	"math"
	"strings"
	"testing"
	"time"
)

func TestOptimizerProgressInterval(t *testing.T) {
	cases := []struct {
		total int
		want  int
	}{
		{3, 1},
		{20, 5},
		{200, 10},
		{500, 25},
	}
	for _, tc := range cases {
		if got := optimizerProgressInterval(tc.total); got != tc.want {
			t.Fatalf("interval(%d) = %d, want %d", tc.total, got, tc.want)
		}
	}
}

func TestFormatOptimizerScore(t *testing.T) {
	if formatOptimizerScore(-1.5) != "-1.5000" {
		t.Fatal("finite score")
	}
	if formatOptimizerScore(math.Inf(-1)) != "-Inf" {
		t.Fatal("neg inf")
	}
}

func TestMarshalRunResultJSONWithInf(t *testing.T) {
	result := &RunResult{
		Timestamp: time.Now(),
		Trials: []TrialResult{{
			Index: 0,
			Score: math.Inf(-1),
		}},
		Best: TrialResult{Index: 0, Score: math.Inf(-1)},
	}
	data, err := marshalRunResultJSON(result)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"score": null`) {
		t.Fatalf("expected null scores in JSON: %s", data)
	}
}
