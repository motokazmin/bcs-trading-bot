package interfaces

import (
	"context"
	"errors"

	"bcs-trading-bot/pkg/models"
)

// ErrNoOpenPosition — закрытие/изменение позиции, которой нет у исполнителя.
// Воркер трактует это как ghost: локальную позицию сбрасывает без retry-спама.
var ErrNoOpenPosition = errors.New("no open position")

// OrderExecutor абстрагирует исполнение ордеров и получение баланса.
// Реализации: VirtualExecutor (paper trading) и BCSClient (реальная торговля).
type OrderExecutor interface {
	ExecuteOrder(ctx context.Context, order models.Order) error
	GetBalance(ctx context.Context) (float64, error)
}
