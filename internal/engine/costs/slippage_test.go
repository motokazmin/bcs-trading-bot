package costs_test

import (
	"math"
	"testing"

	"bcs-trading-bot/internal/engine/costs"
)

func TestFillPriceAlwaysAdverse(t *testing.T) {
	cfg := costs.Config{SlippageBps: 5} // 5 б.п. на ногу

	// Покупка исполняется дороже, продажа дешевле — в обоих случаях против нас.
	if got := costs.FillPrice(cfg, "BUY", 100); math.Abs(got-100.05) > 1e-9 {
		t.Fatalf("BUY: got %.6f, want 100.05", got)
	}
	if got := costs.FillPrice(cfg, "SELL", 100); math.Abs(got-99.95) > 1e-9 {
		t.Fatalf("SELL: got %.6f, want 99.95", got)
	}
}

func TestFillPriceDisabledByDefault(t *testing.T) {
	var zero costs.Config
	if got := costs.FillPrice(zero, "BUY", 100); got != 100 {
		t.Fatalf("без slippage_bps цена не должна меняться, got %.6f", got)
	}
}

// Round-trip: и вход, и выход хуже — суммарные потери равны удвоенному проскальзыванию.
func TestFillPriceRoundTripCostsBothLegs(t *testing.T) {
	cfg := costs.Config{SlippageBps: 10}

	entry := costs.FillPrice(cfg, "BUY", 100)                 // 100.10
	exit := costs.FillPrice(cfg, costs.CloseSide("BUY"), 101) // 100.899
	gross, net := 101.0-100.0, exit-entry
	if net >= gross {
		t.Fatalf("проскальзывание должно уменьшать результат: gross=%.4f net=%.4f", gross, net)
	}
	// 10 б.п. с двух ног от цен ~100 и ~101 ≈ 0.2 руб.
	if lost := gross - net; math.Abs(lost-0.201) > 0.002 {
		t.Fatalf("потери round-trip: got %.4f, want ≈0.201", lost)
	}
}

func TestCloseSideIsOpposite(t *testing.T) {
	if costs.CloseSide("BUY") != "SELL" || costs.CloseSide("SELL") != "BUY" {
		t.Fatal("закрывающая сделка должна быть противоположной")
	}
}
