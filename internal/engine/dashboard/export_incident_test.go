package dashboard

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"bcs-trading-bot/internal/models"
)

// pagingReader повторяет поведение sqlite.Store: при limit<=0 молча ставит 50.
// Именно на этом хендлер выгрузки терял данные — страницу считал за весь период.
type pagingReader struct {
	stubTradeReader
	all      []models.ClosedTrade
	rejected []models.RejectedSignal
}

func (p pagingReader) ListClosedTrades(_ context.Context, _ models.TradeFilter, limit, offset int) (models.TradeListResult, error) {
	if limit <= 0 {
		limit = 50
	}
	if offset > len(p.all) {
		offset = len(p.all)
	}
	end := offset + limit
	if end > len(p.all) {
		end = len(p.all)
	}
	return models.TradeListResult{Trades: p.all[offset:end], Total: len(p.all)}, nil
}

func (p pagingReader) ListRejectedSignals(context.Context, models.TradeFilter) ([]models.RejectedSignal, error) {
	return p.rejected, nil
}

func TestВыгрузкаПериодаОтдаётВсеСделки(t *testing.T) {
	open := time.Date(2026, 9, 10, 11, 40, 0, 0, time.UTC)
	var all []models.ClosedTrade
	for i := 0; i < 120; i++ {
		tr := models.ClosedTrade{
			Ticker: "GAZP", Direction: "BUY", Quantity: 2112,
			OpenedAt: open, ClosedAt: open.Add(time.Minute),
			TradingDate: "2026-09-10",
		}
		// Каждая третья закэплена по кэшу.
		if i%3 == 0 {
			tr.RequestedQuantity = 9540
		}
		all = append(all, tr)
	}
	reader := pagingReader{
		all: all,
		rejected: []models.RejectedSignal{
			{Ticker: "LKOH", Direction: "SELL", Reason: "недостаточно средств", TradingDate: "2026-09-10"},
		},
	}
	srv, err := NewServer(NewHub(), Options{Listen: "127.0.0.1:0", Reader: reader})
	if err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/api/export/incident?date_from=2026-09-10&date_to=2026-09-10", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("код: got %d, want 200 (%s)", rec.Code, rec.Body.String())
	}

	var got IncidentBundle
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("json: %v", err)
	}
	if got.Counts.Trades != 120 {
		t.Fatalf("сделок: got %d, want 120 (limit<=0 дал бы 50 — тихая потеря периода)", got.Counts.Trades)
	}
	if len(got.Trades) != 120 {
		t.Fatalf("len(trades): got %d, want 120", len(got.Trades))
	}
	if got.Counts.CashCapped != 40 {
		t.Fatalf("закэпленных: got %d, want 40", got.Counts.CashCapped)
	}
	if got.Counts.Rejected != 1 || len(got.Rejected) != 1 {
		t.Fatalf("отказов: got %d", got.Counts.Rejected)
	}
	if got.Truncated {
		t.Fatal("truncated не должен взводиться на 120 сделках")
	}
}
