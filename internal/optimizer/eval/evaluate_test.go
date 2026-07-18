package eval_test

import (
	"path/filepath"
	"testing"
	"time"

	"bcs-trading-bot/internal/costs"
	"bcs-trading-bot/internal/marketdata"
	"bcs-trading-bot/internal/optimizer/eval"
	"bcs-trading-bot/pkg/models"
)

func TestLoadCandleDataSkipsMissing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "SBER.csv")
	if err := marketdata.WriteCSV(path, []models.Candle{{
		Timestamp: time.Date(2024, 7, 1, 10, 0, 0, 0, time.UTC),
		Open:      1, High: 2, Low: 0.5, Close: 1.5, Volume: 10,
	}}); err != nil {
		t.Fatal(err)
	}

	data, err := eval.LoadCandleData(dir, []string{"SBER", "YNDX"})
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != 1 {
		t.Fatalf("loaded: got %d tickers, want 1", len(data))
	}
	if _, ok := data["SBER"]; !ok {
		t.Fatal("expected SBER")
	}
}

func TestLoadCandleDataAllMissing(t *testing.T) {
	dir := t.TempDir()
	_, err := eval.LoadCandleData(dir, []string{"YNDX"})
	if err == nil {
		t.Fatal("expected error")
	}
}

// TestAggregateTradesSortsAcrossTickersByTime проверяет, что MaxDrawdown/Calmar
// считаются по хронологии закрытия сделок на уровне всего портфеля, а не по
// порядку "все сделки тикера A, затем все сделки тикера B" (в котором они
// приходят из EvaluatePeriod, т.к. раннер прогоняется по тикерам последовательно).
func TestAggregateTradesSortsAcrossTickersByTime(t *testing.T) {
	t0 := time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC)
	mk := func(closedAt time.Time, pnl float64) models.ClosedTrade {
		return models.ClosedTrade{ClosedAt: closedAt, GrossPnL: pnl, Quantity: 1}
	}

	// Тикер A: прибыль t0 (+200), убыток t0+3 (-50).
	// Тикер B: убыток t0+1 (-150), прибыль t0+2 (+150) — по времени эти
	// сделки находятся МЕЖДУ сделками тикера A.
	tickerAOrder := []models.ClosedTrade{
		mk(t0, 200),
		mk(t0.Add(3*time.Minute), -50),
	}
	tickerBOrder := []models.ClosedTrade{
		mk(t0.Add(1*time.Minute), -150),
		mk(t0.Add(2*time.Minute), 150),
	}

	// Порядок поступления, как в EvaluatePeriod: сначала все сделки A, потом все сделки B.
	concatenated := append(append([]models.ClosedTrade{}, tickerAOrder...), tickerBOrder...)

	got := eval.AggregateTrades(concatenated, costs.Config{}, costs.ClassCodeStocks)

	// Хронологический порядок [+200,-150,+150,-50]: equity 200,50,200,150,
	// просадка от пика 200 до 50 = 150.
	// Порядок "всё A, потом всё B" [+200,-50,-150,+150] дал бы equity
	// 200,150,0,150, просадку 200 — другое число из-за неверного порядка.
	wantDD := 150.0
	if diff := got.MaxDrawdown - wantDD; diff > 1e-9 || diff < -1e-9 {
		t.Fatalf("MaxDrawdown = %.2f, want %.2f (сделки не отсортированы по ClosedAt across тикеров)", got.MaxDrawdown, wantDD)
	}
}
