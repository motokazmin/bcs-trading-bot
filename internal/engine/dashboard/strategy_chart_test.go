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

func TestBuildStrategyTradeChartPayload(t *testing.T) {
	msk := time.FixedZone("MSK", 3*3600)
	open := time.Date(2026, 8, 20, 11, 0, 0, 0, msk)
	closeT := open.Add(20 * time.Minute)
	trade := models.ClosedTrade{
		Ticker: "SBER", Direction: "BUY", ExperimentID: "exp-a",
		EntryPrice: 100, ExitPrice: 105, InitialStopLoss: 95, InitialTakeProfit: 110,
		GrossPnL: 5, PnLR: 1.2, CloseReason: models.CloseReasonTakeProfit,
		OpenedAt: open, ClosedAt: closeT, TradingDate: "2026-08-20", CandleTimeframe: "M5",
	}
	candles := []models.Candle{{
		Ticker: "SBER", Open: 99, High: 106, Low: 98, Close: 105, Timestamp: open,
	}}

	payload := BuildStrategyTradeChartPayload("exp-a", "M5", candles, trade)
	markers, _ := payload["markers"].([]map[string]any)
	if len(markers) != 2 {
		t.Fatalf("markers: %d", len(markers))
	}
	// Вход/выход показываются маркерами; горизонтальные линии-levels
	// намеренно отключены (см. closedTradeLevels) — поле есть, но пустое.
	levels, ok := payload["levels"].([]map[string]any)
	if !ok || len(levels) != 0 {
		t.Fatalf("levels: %#v", payload["levels"])
	}
	if payload["trade_id"] != tradeSpanID(trade) {
		t.Fatalf("trade_id: %v", payload["trade_id"])
	}
}

func TestSingleTradeChartRange(t *testing.T) {
	open := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	closeT := open.Add(10 * time.Minute)
	from, to := singleTradeChartRange(models.ClosedTrade{OpenedAt: open, ClosedAt: closeT})
	if !from.Equal(open.Add(-tradeChartPaddingBefore)) {
		t.Fatalf("from: %v", from)
	}
	if !to.Equal(closeT.Add(tradeChartPaddingAfter)) {
		t.Fatalf("to: %v", to)
	}
}

func TestHandleAPIStrategyTradesAndChart(t *testing.T) {
	msk := time.FixedZone("MSK", 3*3600)
	open := time.Date(2026, 8, 20, 12, 0, 0, 0, msk)
	trade := models.ClosedTrade{
		Ticker: "SBER", Direction: "BUY", ExperimentID: "exp-a",
		EntryPrice: 100, ExitPrice: 102, GrossPnL: 2,
		OpenedAt: open, ClosedAt: open.Add(10 * time.Minute),
		TradingDate: "2026-08-20", CandleTimeframe: "M5",
		CloseReason: models.CloseReasonEOD,
	}
	reader := stubTradeReader{trades: []models.ClosedTrade{trade}}
	fetcher := &stubCandleFetcher{
		candles: []models.Candle{{
			Ticker: "SBER", Open: 100, High: 103, Low: 99, Close: 102, Timestamp: open,
		}},
	}
	srv, err := NewServer(NewHub(), Options{
		Listen:  "127.0.0.1:0",
		Reader:  reader,
		Candles: NewCachedDayCandles(fetcher, "TQBR", time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	h := srv.Handler()

	t.Run("strategy-trades", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/strategy-trades?experiment_id=exp-a", nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
		}
		var body struct {
			ExperimentID string           `json:"experiment_id"`
			TradeCount   int              `json:"trade_count"`
			Trades       []map[string]any `json:"trades"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if body.TradeCount != 1 || len(body.Trades) != 1 {
			t.Fatalf("%+v", body)
		}
		if body.Trades[0]["tradeId"] != tradeSpanID(trade) {
			t.Fatalf("tradeId: %v", body.Trades[0]["tradeId"])
		}
	})

	t.Run("strategy-trade-chart", func(t *testing.T) {
		qs := "/api/strategy-trade-chart?experiment_id=exp-a&ticker=SBER&trade_id=" + tradeSpanID(trade)
		req := httptest.NewRequest(http.MethodGet, qs, nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
		}
		var body map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		markers, _ := body["markers"].([]any)
		if len(markers) != 2 {
			t.Fatalf("markers: %v", body["markers"])
		}
		if _, ok := body["levels"].([]any); !ok {
			t.Fatalf("levels должно быть массивом (пусть и пустым): %#v", body["levels"])
		}
		if fetcher.calls < 1 {
			t.Fatal("fetcher not called")
		}
	})

	t.Run("strategy-trades-missing-id", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/strategy-trades", nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status %d", rec.Code)
		}
	})
}

func TestCachedRangeCandlesTTL(t *testing.T) {
	fetcher := &stubCandleFetcher{
		candles: []models.Candle{{Ticker: "SBER", Close: 1, Timestamp: time.Now()}},
	}
	p := NewCachedDayCandles(fetcher, "TQBR", time.Hour)
	ctx := context.Background()
	from := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	to := from.Add(2 * time.Hour)
	a, err := p.RangeCandles(ctx, "SBER", "M5", from, to)
	if err != nil || len(a) != 1 {
		t.Fatalf("first: %v %v", a, err)
	}
	b, err := p.RangeCandles(ctx, "SBER", "M5", from, to)
	if err != nil || len(b) != 1 {
		t.Fatalf("second: %v %v", b, err)
	}
	if fetcher.calls != 1 {
		t.Fatalf("cache miss: calls=%d", fetcher.calls)
	}
}
