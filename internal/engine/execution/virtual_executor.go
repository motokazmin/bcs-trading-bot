package execution

import (
	"context"
	"fmt"
	"sync"

	"bcs-trading-bot/internal/engine/contract"
	"bcs-trading-bot/internal/models"
)

// ErrTickerAlreadyOpen — повторный open по занятому тикеру (one-position-per-ticker).
var ErrTickerAlreadyOpen = fmt.Errorf("ticker already has open position")

var _ contract.OrderExecutor = (*VirtualExecutor)(nil)

const defaultVirtualBalance = 100_000

// VirtualExecutor симулирует исполнение ордеров в памяти (paper trading).
type VirtualExecutor struct {
	mu        sync.Mutex
	balance   float64
	positions map[string]*virtualPosition
}

type virtualPosition struct {
	direction  string
	quantity   int
	entryPrice float64
	stopLoss   float64
	takeProfit float64
}

func NewVirtualExecutor(initialBalance float64) *VirtualExecutor {
	if initialBalance <= 0 {
		initialBalance = defaultVirtualBalance
	}
	return &VirtualExecutor{
		balance:   initialBalance,
		positions: make(map[string]*virtualPosition),
	}
}

func (v *VirtualExecutor) ExecuteOrder(ctx context.Context, order models.Order) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	v.mu.Lock()
	defer v.mu.Unlock()

	if order.CloseReason != "" {
		return v.closePosition(order)
	}
	return v.openPosition(order)
}

func (v *VirtualExecutor) openPosition(order models.Order) error {
	notional := order.Price * float64(order.Quantity)

	switch order.Direction {
	case "BUY", "SELL":
		if _, exists := v.positions[order.Ticker]; exists {
			return fmt.Errorf("[VIRTUAL] %w: %s", ErrTickerAlreadyOpen, order.Ticker)
		}
		// Как на кэш/марже 1:1: и лонг, и шорт резервируют notional из свободных средств.
		// Шорт больше не раздувает balance (иначе следующий BUY уходит за депозит).
		if v.balance < notional {
			return fmt.Errorf("[VIRTUAL] недостаточно средств: нужно %.2f, доступно %.2f", notional, v.balance)
		}
		v.balance -= notional
		v.positions[order.Ticker] = &virtualPosition{
			direction:  order.Direction,
			quantity:   order.Quantity,
			entryPrice: order.Price,
			stopLoss:   order.StopLoss,
			takeProfit: order.TakeProfit,
		}
	default:
		return fmt.Errorf("[VIRTUAL] неизвестное направление: %s", order.Direction)
	}

	return nil
}

func (v *VirtualExecutor) closePosition(order models.Order) error {
	pos, ok := v.positions[order.Ticker]
	if !ok {
		return fmt.Errorf("[VIRTUAL] нет открытой позиции по %s: %w", order.Ticker, contract.ErrNoOpenPosition)
	}

	closePrice := order.Price
	qty := float64(pos.quantity)
	commission := order.CommissionRub
	entry := pos.entryPrice

	switch pos.direction {
	case "BUY":
		// вернуть выручку продажи
		v.balance += closePrice*qty - commission
	case "SELL":
		// вернуть зарезервированный entry и учесть PnL шорта: +(entry-exit)*qty
		// эквивалент: +(2*entry - exit)*qty - commission
		v.balance += (2*entry-closePrice)*qty - commission
	}

	delete(v.positions, order.Ticker)
	_ = calcVirtualPnL(pos, closePrice)

	return nil
}

func calcVirtualPnL(pos *virtualPosition, closePrice float64) float64 {
	qty := float64(pos.quantity)
	switch pos.direction {
	case "BUY":
		return (closePrice - pos.entryPrice) * qty
	case "SELL":
		return (pos.entryPrice - closePrice) * qty
	default:
		return 0
	}
}

func (v *VirtualExecutor) GetBalance(ctx context.Context) (float64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.balance, nil
}
