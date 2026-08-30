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

type stubTradeReader struct {
	trades []models.ClosedTrade
}

func (s stubTradeReader) ListClosedTrades(_ context.Context, f models.TradeFilter, _, _ int) (models.TradeListResult, error) {
	var out []models.ClosedTrade
	for _, t := range s.trades {
		if f.DateFrom != "" && t.TradingDate < f.DateFrom {
			continue
		}
		if f.DateTo != "" && t.TradingDate > f.DateTo {
			continue
		}
		if f.Ticker != "" && t.Ticker != f.Ticker {
			continue
		}
		out = append(out, t)
	}
	return models.TradeListResult{Trades: out, Total: len(out)}, nil
}

func (stubTradeReader) GetSummary(context.Context, models.TradeFilter) (models.TradeSummary, error) {
	return models.TradeSummary{}, nil
}
func (stubTradeReader) GetBreakdown(context.Context, models.TradeFilter, string) ([]models.BreakdownRow, error) {
	return nil, nil
}
func (stubTradeReader) GetDailyPnL(context.Context, models.TradeFilter) ([]models.DailyPnLRow, error) {
	return nil, nil
}
func (stubTradeReader) GetEquityCurve(context.Context, models.TradeFilter) ([]models.EquityPoint, error) {
	return nil, nil
}
func (stubTradeReader) GetAccountEquity(context.Context, models.TradeFilter, float64) (models.AccountEquity, error) {
	return models.AccountEquity{}, nil
}
func (stubTradeReader) GetDateRange(context.Context, models.TradeFilter) (models.DateRange, error) {
	return models.DateRange{}, nil
}
func (stubTradeReader) ListExperimentIDs(context.Context, models.TradeFilter) ([]string, error) {
	return nil, nil
}

type stubCandleFetcher struct {
	calls   int
	candles []models.Candle
	err     error
}

func (f *stubCandleFetcher) FetchCandles(_ context.Context, _, _, _ string, _, _ time.Time) ([]models.Candle, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return append([]models.Candle(nil), f.candles...), nil
}

func TestBuildDayChartPayloadMarkers(t *testing.T) {
	msk := time.FixedZone("MSK", 3*3600)
	open := time.Date(2026, 8, 20, 11, 0, 0, 0, msk)
	closeT := open.Add(15 * time.Minute)
	trades := []models.ClosedTrade{
		{
			Ticker: "SBER", Direction: "BUY", EntryPrice: 100, ExitPrice: 105,
			GrossPnL: 5, CloseReason: models.CloseReasonTakeProfit,
			OpenedAt: open, ClosedAt: closeT, TradingDate: "2026-08-20",
			CandleTimeframe: "M5", ExperimentID: "exp1",
		},
		{
			Ticker: "SBER", Direction: "SELL", EntryPrice: 110, ExitPrice: 108,
			GrossPnL: -2, CloseReason: models.CloseReasonStopLoss,
			OpenedAt: open.Add(time.Hour), ClosedAt: open.Add(70 * time.Minute),
			TradingDate: "2026-08-20",
		},
	}
	candles := []models.Candle{{
		Ticker: "SBER", Open: 99, High: 106, Low: 98, Close: 105, Timestamp: open,
	}}

	payload := BuildDayChartPayload("2026-08-20", "SBER", "M5", candles, trades)
	markers, _ := payload["markers"].([]map[string]any)
	if len(markers) != 4 {
		t.Fatalf("markers: got %d, want 4", len(markers))
	}
	if markers[0]["text"] != "1 ВХ 100.00" {
		t.Fatalf("entry text: %v", markers[0]["text"])
	}
	if markers[1]["text"] != "1 ВЫХ TP 105.00" {
		t.Fatalf("exit text: %v", markers[1]["text"])
	}
	if markers[0]["shape"] != "arrowUp" {
		t.Fatalf("long entry shape: %v", markers[0]["shape"])
	}
	spans, _ := payload["trades"].([]map[string]any)
	if len(spans) != 2 {
		t.Fatalf("trades: %d", len(spans))
	}
	if spans[0]["entryLabel"] != "11:00" {
		t.Fatalf("entryLabel: %v", spans[0]["entryLabel"])
	}
	if payload["timeframe"] != "M5" {
		t.Fatalf("timeframe: %v", payload["timeframe"])
	}
}

func TestBuildDayChartPayloadEmpty(t *testing.T) {
	payload := BuildDayChartPayload("2026-08-20", "GAZP", "M5", nil, nil)
	candles, _ := payload["candles"].([]map[string]any)
	if candles == nil || len(candles) != 0 {
		t.Fatalf("candles: %v", payload["candles"])
	}
	markers, _ := payload["markers"].([]map[string]any)
	if len(markers) != 0 {
		t.Fatalf("markers: %d", len(markers))
	}
}

func TestCachedDayCandlesTTL(t *testing.T) {
	fetcher := &stubCandleFetcher{
		candles: []models.Candle{{Ticker: "SBER", Close: 1, Timestamp: time.Now()}},
	}
	p := NewCachedDayCandles(fetcher, "TQBR", time.Hour)
	ctx := context.Background()
	a, err := p.DayCandles(ctx, "sber", "M5", "2026-08-20")
	if err != nil || len(a) != 1 {
		t.Fatalf("first: %v %v", a, err)
	}
	b, err := p.DayCandles(ctx, "SBER", "M5", "2026-08-20")
	if err != nil || len(b) != 1 {
		t.Fatalf("second: %v %v", b, err)
	}
	if fetcher.calls != 1 {
		t.Fatalf("cache miss: calls=%d", fetcher.calls)
	}
}

func TestHandleAPIDayTradesAndChart(t *testing.T) {
	msk := time.FixedZone("MSK", 3*3600)
	open := time.Date(2026, 8, 20, 12, 0, 0, 0, msk)
	reader := stubTradeReader{trades: []models.ClosedTrade{
		{
			Ticker: "SBER", Direction: "BUY", EntryPrice: 100, ExitPrice: 102,
			GrossPnL: 2, OpenedAt: open, ClosedAt: open.Add(10 * time.Minute),
			TradingDate: "2026-08-20", CandleTimeframe: "M5", ExperimentID: "a",
			CloseReason: models.CloseReasonEOD,
		},
		{
			Ticker: "GAZP", Direction: "SELL", EntryPrice: 150, ExitPrice: 149,
			GrossPnL: 1, OpenedAt: open, ClosedAt: open.Add(5 * time.Minute),
			TradingDate: "2026-08-20",
		},
	}}
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

	t.Run("day-trades", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/day-trades?date=2026-08-20", nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
		}
		var body struct {
			Date       string             `json:"date"`
			TradeCount int                `json:"trade_count"`
			Tickers    []DayTickerSummary `json:"tickers"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if body.TradeCount != 2 || len(body.Tickers) != 2 {
			t.Fatalf("%+v", body)
		}
	})

	t.Run("day-chart", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/day-chart?date=2026-08-20&ticker=SBER", nil)
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
		trades, _ := body["trades"].([]any)
		if len(trades) != 1 {
			t.Fatalf("trades: %v", body["trades"])
		}
		if body["timeframe"] != "M5" {
			t.Fatalf("timeframe: %v", body["timeframe"])
		}
		if fetcher.calls < 1 {
			t.Fatal("fetcher not called")
		}
	})

	t.Run("day-chart-bad-date", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/day-chart?date=bad&ticker=SBER", nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status %d", rec.Code)
		}
	})
}

func TestResolveDayTimeframe(t *testing.T) {
	if got := resolveDayTimeframe(nil, ""); got != "M5" {
		t.Fatalf("default: %s", got)
	}
	if got := resolveDayTimeframe([]models.ClosedTrade{{CandleTimeframe: "m1"}}, ""); got != "M1" {
		t.Fatalf("from trade: %s", got)
	}
	if got := resolveDayTimeframe([]models.ClosedTrade{{CandleTimeframe: "M5"}}, "H1"); got != "H1" {
		t.Fatalf("override: %s", got)
	}
}
