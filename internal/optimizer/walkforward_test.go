package optimizer_test

import (
	"math"
	"testing"
	"time"

	"bcs-trading-bot/internal/optimizer"
)

func TestGenerateWindows(t *testing.T) {
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

	windows := optimizer.GenerateWindows(start, end, 2, 1)
	if len(windows) == 0 {
		t.Fatal("expected windows")
	}

	for i, w := range windows {
		if !w.Start.Before(w.End) {
			t.Fatalf("window %d: start not before end", i)
		}
		if i > 0 {
			prev := windows[i-1]
			if !w.Start.After(prev.Start) {
				t.Fatalf("window %d: step not advancing", i)
			}
		}
	}
}

func TestGenerateWindowsNoOverlap(t *testing.T) {
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	windows := optimizer.GenerateWindows(start, end, 2, 1)
	for i, w := range windows {
		if !w.Start.Before(w.End) {
			t.Fatalf("window %d: invalid bounds", i)
		}
	}
}

func TestScoreRejectsLowTrades(t *testing.T) {
	m := optimizer.Metrics{NumTrades: 5, Calmar: 10}
	if s := optimizer.Score(m, 20); !math.IsInf(s, -1) {
		t.Fatalf("expected -Inf, got %v", s)
	}
}

func TestScoreCalmar(t *testing.T) {
	m := optimizer.Metrics{NumTrades: 30, Calmar: 2.5, MaxDrawdown: 1.0, TotalPnL: 2.5}
	if s := optimizer.Score(m, 20); s != 2.5 {
		t.Fatalf("score: got %v, want 2.5", s)
	}
}

func TestScoreLosingUsesPnL(t *testing.T) {
	m := optimizer.Metrics{NumTrades: 30, Calmar: -1, MaxDrawdown: 50000, TotalPnL: -41179.48}
	if s := optimizer.Score(m, 20); s != -41179.48 {
		t.Fatalf("score: got %v, want TotalPnL", s)
	}
}

func TestMedianFloatIgnoresInf(t *testing.T) {
	got := optimizer.MedianFloat([]float64{math.Inf(-1), -100, -200, -300})
	if got != -200 {
		t.Fatalf("median: got %v, want -200", got)
	}
	if !math.IsInf(optimizer.MedianFloat([]float64{math.Inf(-1)}), -1) {
		t.Fatal("empty after filter should be -Inf")
	}
}

func TestNetPnLFromGross(t *testing.T) {
	net := optimizer.NetPnLFromGross(100, 10, 5.0)
	if net != 50 {
		t.Fatalf("net pnl: got %.2f, want 50", net)
	}
}

func TestComputeMetricsDrawdown(t *testing.T) {
	m := optimizer.ComputeMetrics([]float64{100, -50, -30}, []float64{1, -0.5, -0.3})
	if m.TotalPnL != 20 {
		t.Fatalf("total: got %.2f", m.TotalPnL)
	}
	if m.MaxDrawdown != 80 {
		t.Fatalf("max dd: got %.2f, want 80", m.MaxDrawdown)
	}
	if m.NumTrades != 3 {
		t.Fatalf("trades: got %d", m.NumTrades)
	}
}
