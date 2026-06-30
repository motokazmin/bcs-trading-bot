package engine

import (
	"context"
	"fmt"
	"time"

	"bcs-trading-bot/internal/bcs"
	"bcs-trading-bot/pkg/interfaces"
	"bcs-trading-bot/pkg/logx"
	"bcs-trading-bot/pkg/models"
)

const smokeTestTimeout = 90 * time.Second

// RunSmokeTest проверяет OAuth, WebSocket и виртуальное исполнение:
// ждёт первую котировку, открывает и сразу закрывает 1 лот без записи в БД.
func RunSmokeTest(ctx context.Context, client *bcs.BCSClient, ticker string, executor interfaces.OrderExecutor) error {
	logx.Info("Smoke-test: ожидание котировки по %s (таймаут %s)...", ticker, smokeTestTimeout)

	tickCh := make(chan models.Tick, 8)
	routes := map[string][]bcs.WorkerRoutes{
		ticker: {{TickChan: tickCh}},
	}

	ctx, cancel := context.WithTimeout(ctx, smokeTestTimeout)
	defer cancel()

	wsDone := make(chan error, 1)
	go func() {
		wsDone <- client.SubscribeMarketDataFanOut(ctx, routes)
	}()

	var tick models.Tick
	select {
	case tick = <-tickCh:
		logx.Info("Smoke-test: котировка получена — %s @ %.2f", tick.Ticker, tick.Price)
	case err := <-wsDone:
		if err != nil && err != context.Canceled && err != context.DeadlineExceeded {
			return fmt.Errorf("websocket: %w", err)
		}
		return fmt.Errorf("websocket завершился до получения котировки")
	case <-ctx.Done():
		return fmt.Errorf("таймаут %s: нет котировки (рынок закрыт или нет данных)", smokeTestTimeout)
	}

	cancel()

	if err := runSmokeCycle(ticker, tick.Price, executor); err != nil {
		return err
	}

	logx.Info("Smoke-test: OK — OAuth, WebSocket и виртуальное исполнение работают")
	return nil
}

func runSmokeCycle(ticker string, price float64, executor interfaces.OrderExecutor) error {
	const qty = 1
	risk := price * 0.01

	open := models.Order{
		Ticker:     ticker,
		Direction:  "BUY",
		Quantity:   qty,
		Price:      price,
		StopLoss:   price - risk,
		TakeProfit: price + 3*risk,
		OrderType:  models.OrderTypeLimit,
	}
	if err := executor.ExecuteOrder(open); err != nil {
		return fmt.Errorf("smoke open: %w", err)
	}
	logx.TradeOpen(ticker, open.Direction, qty, price, open.StopLoss, open.TakeProfit)

	close := models.Order{
		Ticker:      ticker,
		Direction:   "SELL",
		Quantity:    qty,
		Price:       price,
		OrderType:   models.OrderTypeMarket,
		CloseReason: models.CloseReasonSmoke,
	}
	if err := executor.ExecuteOrder(close); err != nil {
		return fmt.Errorf("smoke close: %w", err)
	}
	logx.TradeClose(ticker, models.CloseReasonSmoke, price, 0, 0)

	return nil
}
