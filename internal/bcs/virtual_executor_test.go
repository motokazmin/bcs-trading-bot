package bcs

import (
	"math"
	"testing"

	"bcs-trading-bot/pkg/models"
)

func TestVirtualExecutorLongRoundTrip(t *testing.T) {
	v := NewVirtualExecutor(100_000)

	open := models.Order{
		Ticker:     "SBER",
		Direction:  "BUY",
		Quantity:   10,
		Price:      300,
		StopLoss:   295,
		TakeProfit: 315,
	}
	if err := v.ExecuteOrder(open); err != nil {
		t.Fatalf("open: %v", err)
	}

	close := models.Order{
		Ticker:      "SBER",
		Direction:   "SELL",
		Quantity:    10,
		Price:       305,
		CloseReason: models.CloseReasonStopLoss,
	}
	if err := v.ExecuteOrder(close); err != nil {
		t.Fatalf("close: %v", err)
	}

	balance, err := v.GetBalance()
	if err != nil {
		t.Fatalf("GetBalance: %v", err)
	}
	want := 100_000.0 + (305-300)*10
	if math.Abs(balance-want) > 0.01 {
		t.Fatalf("balance = %.2f, want %.2f", balance, want)
	}
}

func TestVirtualExecutorShortRoundTrip(t *testing.T) {
	v := NewVirtualExecutor(200_000)

	open := models.Order{
		Ticker:     "TATN",
		Direction:  "SELL",
		Quantity:   43,
		Price:      463.90,
		StopLoss:   466.22,
		TakeProfit: 456.94,
	}
	if err := v.ExecuteOrder(open); err != nil {
		t.Fatalf("open: %v", err)
	}

	close := models.Order{
		Ticker:      "TATN",
		Direction:   "BUY",
		Quantity:    43,
		Price:       466.30,
		CloseReason: models.CloseReasonStopLoss,
	}
	if err := v.ExecuteOrder(close); err != nil {
		t.Fatalf("close: %v", err)
	}

	balance, err := v.GetBalance()
	if err != nil {
		t.Fatalf("GetBalance: %v", err)
	}
	pnl := (463.90 - 466.30) * 43
	want := 200_000.0 + pnl
	if math.Abs(balance-want) > 0.01 {
		t.Fatalf("balance = %.2f, want %.2f (pnl %.2f)", balance, want, pnl)
	}
}
