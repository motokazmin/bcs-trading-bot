package engine

import (
	"context"
	"testing"

	"bcs-trading-bot/pkg/interfaces"
	"bcs-trading-bot/pkg/models"
)

type smokeExecutor struct {
	orders []models.Order
}

func (e *smokeExecutor) ExecuteOrder(_ context.Context, order models.Order) error {
	e.orders = append(e.orders, order)
	return nil
}

func (e *smokeExecutor) GetBalance(context.Context) (float64, error) { return 0, nil }

var _ interfaces.OrderExecutor = (*smokeExecutor)(nil)

func TestRunSmokeCycle(t *testing.T) {
	exec := &smokeExecutor{}
	if err := runSmokeCycle(context.Background(), "SBER", 300.0, exec); err != nil {
		t.Fatal(err)
	}
	if len(exec.orders) != 2 {
		t.Fatalf("orders: got %d, want 2", len(exec.orders))
	}
	if exec.orders[0].Direction != "BUY" || exec.orders[0].CloseReason != "" {
		t.Fatalf("open: %+v", exec.orders[0])
	}
	if exec.orders[1].Direction != "SELL" || exec.orders[1].CloseReason != models.CloseReasonSmoke {
		t.Fatalf("close: %+v", exec.orders[1])
	}
}