package interfaces

import "bcs-trading-bot/pkg/models"

// OrderExecutor абстрагирует исполнение ордеров и получение баланса.
// Реализации: VirtualExecutor (paper trading) и BCSClient (реальная торговля).
type OrderExecutor interface {
	ExecuteOrder(order models.Order) error
	GetBalance() (float64, error)
}
