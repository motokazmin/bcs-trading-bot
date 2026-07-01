package interfaces

import (
	"context"

	"bcs-trading-bot/pkg/models"
)

// OrderExecutor абстрагирует исполнение ордеров и получение баланса.
// Реализации: VirtualExecutor (paper trading) и BCSClient (реальная торговля).
type OrderExecutor interface {
	ExecuteOrder(ctx context.Context, order models.Order) error
	GetBalance(ctx context.Context) (float64, error)
}
