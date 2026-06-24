package bcs

import (
	"fmt"
	"log"
	"math"
	"sync"

	"bcs-trading-bot/pkg/interfaces"
	"bcs-trading-bot/pkg/models"
)

var _ interfaces.OrderExecutor = (*VirtualExecutor)(nil)

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

func (v *VirtualExecutor) ExecuteOrder(order models.Order) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	if order.CloseReason != "" {
		return v.closePosition(order)
	}
	return v.openPosition(order)
}

func (v *VirtualExecutor) openPosition(order models.Order) error {
	notional := order.Price * float64(order.Quantity)
	risk := math.Abs(order.Price-order.StopLoss) * float64(order.Quantity)
	potentialProfit := math.Abs(order.TakeProfit-order.Price) * float64(order.Quantity)

	switch order.Direction {
	case "BUY":
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
	case "SELL":
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

	log.Printf(
		"[VIRTUAL] ОТКРЫТИЕ %s %s x%d @ %.2f | баланс=%.2f | риск(SL)=%.2f | потенциал(TP)=%.2f | SL=%.2f TP=%.2f",
		order.Direction, order.Ticker, order.Quantity, order.Price,
		v.balance, risk, potentialProfit, order.StopLoss, order.TakeProfit,
	)
	return nil
}

func (v *VirtualExecutor) closePosition(order models.Order) error {
	pos, ok := v.positions[order.Ticker]
	if !ok {
		return fmt.Errorf("[VIRTUAL] нет открытой позиции по %s", order.Ticker)
	}

	closePrice := order.Price
	qty := float64(pos.quantity)
	pnl := calcVirtualPnL(pos, closePrice)

	switch pos.direction {
	case "BUY":
		v.balance += closePrice * qty
	case "SELL":
		v.balance -= closePrice * qty
	}

	delete(v.positions, order.Ticker)

	switch order.CloseReason {
	case models.CloseReasonStopLoss:
		if pnl < 0 {
			log.Printf("[VIRTUAL] [STOP-LOSS СРАБОТАЛ] Тикер: %s, Убыток: %.2f, Новый баланс: %.2f",
				order.Ticker, -pnl, v.balance)
		} else {
			log.Printf("[VIRTUAL] [STOP-LOSS СРАБОТАЛ] Тикер: %s, Результат: %.2f, Новый баланс: %.2f",
				order.Ticker, pnl, v.balance)
		}
	case models.CloseReasonTakeProfit:
		log.Printf("[VIRTUAL] [TAKE-PROFIT СРАБОТАЛ] Тикер: %s, Прибыль: %.2f, Новый баланс: %.2f",
			order.Ticker, pnl, v.balance)
	case models.CloseReasonEOD:
		log.Printf("[VIRTUAL] [EOD ЗАКРЫТИЕ] Тикер: %s, PnL: %.2f, Новый баланс: %.2f",
			order.Ticker, pnl, v.balance)
	default:
		log.Printf("[VIRTUAL] ЗАКРЫТИЕ %s %s x%d @ %.2f | PnL=%.2f | баланс=%.2f",
			order.Direction, order.Ticker, order.Quantity, closePrice, pnl, v.balance)
	}

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

func (v *VirtualExecutor) GetBalance() (float64, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.balance, nil
}
