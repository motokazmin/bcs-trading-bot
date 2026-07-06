package optimizer

import (
	"testing"
)

func TestIntersectTickers(t *testing.T) {
	got := intersectTickers([]string{"SBER", "GAZP", "LKOH"}, []string{"SBER", "ROSN", "NVTK"})
	if len(got) != 1 || got[0] != "SBER" {
		t.Fatalf("intersect: %v", got)
	}
}

func TestSameTickerSets(t *testing.T) {
	if !sameTickerSets([]string{"A", "B"}, []string{"B", "A"}) {
		t.Fatal("expected same sets")
	}
	if sameTickerSets([]string{"A"}, []string{"A", "B"}) {
		t.Fatal("expected different sets")
	}
}

func TestResolvePhase1Tickers(t *testing.T) {
	full := []string{"SBER", "GAZP", "ROSN"}
	got, err := ResolvePhase1Tickers("", []string{"SBER", "ROSN", "NVTK"}, full)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("lean intersect: %v", got)
	}

	override, err := ResolvePhase1Tickers("SBER", nil, full)
	if err != nil {
		t.Fatal(err)
	}
	if len(override) != 1 || override[0] != "SBER" {
		t.Fatalf("override: %v", override)
	}
}
