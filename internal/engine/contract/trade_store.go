package contract

import (
	"context"

	"bcs-trading-bot/internal/models"
)

// TradeStore сохраняет закрытые сделки для последующего анализа.
type TradeStore interface {
	SaveClosedTrade(ctx context.Context, trade models.ClosedTrade) error
	Close() error
}

// TradeReader читает сделки и агрегаты (админка, экспорт для ИИ).
type TradeReader interface {
	ListClosedTrades(ctx context.Context, f models.TradeFilter, limit, offset int) (models.TradeListResult, error)
	GetSummary(ctx context.Context, f models.TradeFilter) (models.TradeSummary, error)
	GetBreakdown(ctx context.Context, f models.TradeFilter, groupBy string) ([]models.BreakdownRow, error)
	GetDailyPnL(ctx context.Context, f models.TradeFilter) ([]models.DailyPnLRow, error)
	GetEquityCurve(ctx context.Context, f models.TradeFilter) ([]models.EquityPoint, error)
	GetAccountEquity(ctx context.Context, f models.TradeFilter, startingDeposit float64) (models.AccountEquity, error)
	GetDateRange(ctx context.Context, f models.TradeFilter) (models.DateRange, error)
	ListExperimentIDs(ctx context.Context, f models.TradeFilter) ([]string, error)
}

// NoopTradeStore отключает персистентность (тесты, storage.enabled=false).
type NoopTradeStore struct{}

func (NoopTradeStore) SaveClosedTrade(context.Context, models.ClosedTrade) error { return nil }
func (NoopTradeStore) Close() error                                              { return nil }
