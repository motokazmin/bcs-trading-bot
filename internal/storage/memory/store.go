package memory

import (
	"context"
	"sync"

	"bcs-trading-bot/internal/models"
)

// TradeStore — in-memory хранилище закрытых сделок (backtest, тесты).
type TradeStore struct {
	mu     sync.Mutex
	trades []models.ClosedTrade
}

func NewTradeStore() *TradeStore {
	return &TradeStore{}
}

func (s *TradeStore) SaveClosedTrade(_ context.Context, trade models.ClosedTrade) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.trades = append(s.trades, trade)
	return nil
}

func (s *TradeStore) Close() error { return nil }

func (s *TradeStore) Trades() []models.ClosedTrade {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]models.ClosedTrade, len(s.trades))
	copy(out, s.trades)
	return out
}

func (s *TradeStore) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.trades = nil
}
