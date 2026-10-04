package core_test

import (
	"math"
	"testing"

	"bcs-trading-bot/internal/optimizer/core"
)

// Редкий набор: из 16 окон сделок хватило в одном, и оно удачное. По прежнему правилу
// он получал оценку этого окна и обгонял набор, торгующий во всех окнах (0014: OR Fade
// со score 8.9 по одному окну, на хвосте −0.192R).
func TestWindowScoreОтсеиваетРедкийНабор(t *testing.T) {
	inf := math.Inf(-1)
	rare := []float64{inf, inf, inf, 8.9, inf, inf, inf, inf, inf, inf, inf, inf, inf, inf, inf, inf}
	if got := core.WindowScore(rare, 0); got != 8.9 {
		t.Fatalf("при 0 — прежнее правило, медиана по прошедшим окнам: %v", got)
	}
	if got := core.WindowScore(rare, 0.75); !math.IsInf(got, -1) {
		t.Fatalf("1 окно из 16 при планке 0.75 должно отсеиваться, score=%v", got)
	}

	// 12 из 16 = ровно 0.75: проходит, медиана — по прошедшим окнам.
	edge := []float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, inf, inf, inf, inf}
	if got := core.WindowScore(edge, 0.75); got != 6.5 {
		t.Fatalf("12/16 при 0.75 должно проходить с медианой 6.5, score=%v", got)
	}
	if got := core.WindowScore(edge[1:], 0.75); !math.IsInf(got, -1) {
		t.Fatalf("11/15 < 0.75 должно отсеиваться, score=%v", got)
	}
}
