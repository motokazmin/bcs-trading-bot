package backtest

import (
	"context"
	"testing"
	"time"

	"bcs-trading-bot/internal/config"
	"bcs-trading-bot/internal/engine/execution"
	"bcs-trading-bot/internal/engine/risk"
	"bcs-trading-bot/internal/engine/storage/memory"
	"bcs-trading-bot/internal/models"
	"bcs-trading-bot/internal/strategy"
)

// onceStrategy входит один раз по close первого бара, на котором его спросили.
type onceStrategy struct{ done bool }

func (s *onceStrategy) ID() string { return "once" }
func (s *onceStrategy) OnCandle(c models.Candle) *models.Order {
	if s.done {
		return nil
	}
	s.done = true
	return &models.Order{Direction: "BUY", Price: c.Close, StopLoss: c.Close * 0.9, TakeProfit: c.Close * 1.1}
}

// runBoth гоняет свечи одного тикера через оба пути backtest.
func runBoth(t *testing.T, name string, sess config.SessionConfig, candles []models.Candle) (*memory.TradeStore, bool) {
	t.Helper()
	return runBothWith(t, name, sess, candles, &onceStrategy{})
}

func runBothWith(t *testing.T, name string, sess config.SessionConfig, candles []models.Candle, strat strategy.CandleStrategy) (*memory.TradeStore, bool) {
	t.Helper()
	store := memory.NewTradeStore()
	cfg := RunnerConfig{
		Ticker: "SBER", CandleTimeframe: "M5", StepPriceValue: 1, Deposit: 1e6, MaxDailyLoss: 1e6,
		RiskPerTradePct: 0.1, Strategy: strat, SessionCfg: sess,
	}
	exec := execution.NewVirtualExecutor(1e6)
	if name == "runner" {
		r, err := NewRunner(cfg, store)
		if err != nil {
			t.Fatal(err)
		}
		if err := r.Run(context.Background(), candles, exec); err != nil {
			t.Fatal(err)
		}
		return store, r.position != nil
	}
	p, err := NewPortfolioRunner(PortfolioRunnerConfig{
		Tickers: map[string]RunnerConfig{"once/SBER": cfg}, SessionCfg: sess,
		GlobalRisk: risk.NewGlobalRiskController(1e6, 100, 0.1, 5),
	}, store)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Run(context.Background(), map[string][]models.Candle{"SBER": candles}, exec); err != nil {
		t.Fatal(err)
	}
	return store, p.states["once/SBER"].position != nil
}

func mskBar(t *testing.T, date, hhmm string, px float64) models.Candle {
	t.Helper()
	msk, _ := time.LoadLocation("Europe/Moscow")
	ts, err := time.ParseInLocation("2006-01-02 15:04", date+" "+hhmm, msk)
	if err != nil {
		t.Fatal(err)
	}
	return models.Candle{Ticker: "SBER", Open: px, High: px + 0.1, Low: px - 0.1, Close: px, Timestamp: ts.UTC()}
}

// Решения backtest — на закрытии бара, как в live (метка бара — его начало).
// Сессия 19:05–23:50 МСК, бары 23:35, 23:40, 23:45 (последний заканчивается ровно
// в 23:50). Раньше EOD проверялся по началу бара, бара с началом ≥ 23:50 в данных
// нет, и session-orc-evening держал позиции через ночь (27 сделок из 156).
func TestEODClosesOnBarEndingAtEOD(t *testing.T) {
	sess := config.SessionConfig{Timezone: "Europe/Moscow", SessionOpenTime: "19:05", EODCloseTime: "23:50"}
	candles := []models.Candle{
		mskBar(t, "2026-09-01", "23:35", 100), mskBar(t, "2026-09-01", "23:40", 100.5),
		mskBar(t, "2026-09-01", "23:45", 101),
	}
	// Следующего бара нет: закрыть обязан сам бар, заканчивающийся на eod, а не
	// страховка closeMissedEOD на следующем баре.
	for _, name := range []string{"runner", "portfolio"} {
		t.Run(name, func(t *testing.T) {
			store, open := runBoth(t, name, sess, candles)
			trades := store.Trades()
			if open || len(trades) != 1 {
				t.Fatalf("позиция должна закрыться на баре 23:45–23:50: open=%v сделок=%d", open, len(trades))
			}
			if tr := trades[0]; tr.CloseReason != models.CloseReasonEOD || tr.ExitPrice != 101 || tr.EntryPrice != 100 {
				t.Fatalf("вход 100, EOD по close бара 23:45–23:50 (101): %s %.2f→%.2f", tr.CloseReason, tr.EntryPrice, tr.ExitPrice)
			}
		})
	}
}

// Дыра в данных: последний бар дня закончился раньше eod, следующий — уже утром.
// В live позицию закрыл бы таймер EOD по последней цене; backtest закрывает по close
// последнего бара, а не держит через ночь до утреннего гэпа.
func TestMissedEODClosesAtLastClose(t *testing.T) {
	sess := config.SessionConfig{Timezone: "Europe/Moscow", SessionOpenTime: "19:05", EODCloseTime: "23:50"}
	candles := []models.Candle{
		mskBar(t, "2026-09-01", "23:20", 100), mskBar(t, "2026-09-01", "23:25", 102),
		mskBar(t, "2026-09-02", "07:00", 95),
	}
	for _, name := range []string{"runner", "portfolio"} {
		t.Run(name, func(t *testing.T) {
			store, _ := runBoth(t, name, sess, candles)
			trades := store.Trades()
			if len(trades) != 1 || trades[0].CloseReason != models.CloseReasonEOD || trades[0].ExitPrice != 102 {
				t.Fatalf("EOD по последнему close дня 102, а не утром 95: %+v", trades)
			}
		})
	}
}

// Бар, заканчивающийся ровно на eod_close_time, входа не даёт: в live он приходит
// после eod, EntriesAllowed(now) уже false. Раньше backtest проверял начало бара и
// брал сделку, которую live не возьмёт (−28 сделок за 2024-07→2026-10).
func TestNoEntryOnBarEndingAtEOD(t *testing.T) {
	sess := config.SessionConfig{Timezone: "Europe/Moscow", SessionOpenTime: "07:00", EODCloseTime: "09:50"}
	for _, name := range []string{"runner", "portfolio"} {
		t.Run(name, func(t *testing.T) {
			store, open := runBoth(t, name, sess, []models.Candle{mskBar(t, "2026-09-01", "09:45", 100)})
			if open || len(store.Trades()) != 0 {
				t.Fatal("бар 09:45–09:50 при eod 09:50 не должен давать вход")
			}
		})
	}
}
