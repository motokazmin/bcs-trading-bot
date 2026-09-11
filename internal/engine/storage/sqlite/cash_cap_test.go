package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"bcs-trading-bot/internal/models"
)

// Объём ДО капа по кэшу обязан доезжать до БД и обратно. Без этой пары
// (requested_quantity / quantity) факт капа выводится только косвенно — через
// разброс gross_pnl/pnl_r между сделками, и однажды это стоило целого круга
// разбора. См. docs/analysis/0002-live-config-drift-and-cash-sizing.md
func TestКапПоКэшуДоезжаетДоБД(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "trades.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	opened := time.Date(2026, 9, 10, 11, 40, 0, 0, dbLoc)
	trade := models.ClosedTrade{
		TradingMode: "virtual", RunID: "r", ExperimentID: "orc-complement",
		Ticker: "GAZP", ClassCode: "TQBR", StepPriceValue: 1, Direction: "BUY",
		Quantity: 2112, EntryPrice: 89.109, ExitPrice: 89.263,
		RDistance: 0.114, GrossPnL: 295.46, PnLR: 1.23,
		OpenedAt: opened, ClosedAt: opened.Add(20 * time.Minute), HoldSeconds: 1193,
		TradingDate: "2026-09-10",
		// Риск-менеджер запросил 9540, кэш позволил 2112.
		RequestedQuantity: 9540, CashAtOpen: 198110.61, BarAgeSeconds: 2,
	}
	if err := store.SaveClosedTrade(context.Background(), trade); err != nil {
		t.Fatalf("SaveClosedTrade: %v", err)
	}

	res, err := store.ListClosedTrades(context.Background(), models.TradeFilter{}, 0, 0)
	if err != nil {
		t.Fatalf("ListClosedTrades: %v", err)
	}
	if len(res.Trades) != 1 {
		t.Fatalf("сделок: got %d, want 1", len(res.Trades))
	}
	got := res.Trades[0]
	if got.RequestedQuantity != 9540 {
		t.Fatalf("requested_quantity: got %d, want 9540", got.RequestedQuantity)
	}
	if got.CashAtOpen != 198110.61 {
		t.Fatalf("cash_at_open: got %v, want 198110.61", got.CashAtOpen)
	}
	if got.BarAgeSeconds != 2 {
		t.Fatalf("bar_age_seconds: got %v, want 2", got.BarAgeSeconds)
	}
	if !got.CashCapped() {
		t.Fatalf("CashCapped: got false при quantity=%d < requested=%d", got.Quantity, got.RequestedQuantity)
	}
}

// Отказы должны переживать круговой проход и фильтроваться тем же фильтром,
// что и сделки: иначе выборки «сделки» и «отказы» разъедутся.
func TestОтказыСохраняютсяИФильтруются(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "trades.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	for _, r := range []models.RejectedSignal{
		{ExperimentID: "orc-complement", Ticker: "GAZP", Direction: "BUY",
			Reason: "недостаточно средств", RequestedQty: 9540, CashAtCheck: 11213.29,
			TradingDate: "2026-09-10", RejectedAt: time.Date(2026, 9, 10, 11, 40, 0, 0, dbLoc)},
		{ExperimentID: "or-fade-conservative", Ticker: "CHMF", Direction: "SELL",
			Reason: "дневной лимит убытков", TradingDate: "2026-09-11",
			RejectedAt: time.Date(2026, 9, 11, 12, 0, 0, 0, dbLoc)},
	} {
		if err := store.SaveRejectedSignal(ctx, r); err != nil {
			t.Fatalf("SaveRejectedSignal: %v", err)
		}
	}

	all, err := store.ListRejectedSignals(ctx, models.TradeFilter{})
	if err != nil {
		t.Fatalf("ListRejectedSignals: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("всего отказов: got %d, want 2", len(all))
	}

	one, err := store.ListRejectedSignals(ctx, models.TradeFilter{ExperimentID: "orc-complement"})
	if err != nil {
		t.Fatalf("ListRejectedSignals filtered: %v", err)
	}
	if len(one) != 1 || one[0].Ticker != "GAZP" || one[0].RequestedQty != 9540 {
		t.Fatalf("фильтр по эксперименту: got %+v", one)
	}

	// Архивный период вырезается так же, как у сделок.
	hidden, err := store.ListRejectedSignals(ctx, models.TradeFilter{
		ExcludeRanges: []models.DateRange{{From: "2026-09-10", To: "2026-09-10"}},
	})
	if err != nil {
		t.Fatalf("ListRejectedSignals excluded: %v", err)
	}
	if len(hidden) != 1 || hidden[0].Ticker != "CHMF" {
		t.Fatalf("ExcludeRanges не применился: got %+v", hidden)
	}
}
