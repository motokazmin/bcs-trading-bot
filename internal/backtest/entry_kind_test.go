package backtest

import (
	"context"
	"math"
	"testing"

	"bcs-trading-bot/internal/config"
	"bcs-trading-bot/internal/engine/costs"
	"bcs-trading-bot/internal/engine/execution"
	"bcs-trading-bot/internal/engine/risk"
	"bcs-trading-bot/internal/engine/storage/memory"
	"bcs-trading-bot/internal/models"
)

// fillStrategy один раз отдаёт заранее заданную заявку.
type fillStrategy struct {
	order *models.Order
	done  bool
}

func (s *fillStrategy) ID() string { return "fill" }
func (s *fillStrategy) OnCandle(models.Candle) *models.Order {
	if s.done {
		return nil
	}
	s.done = true
	o := *s.order
	return &o
}

// Лимит, исполнившийся внутри бара ровно по его close, остаётся лимитом: стоп, задетый в
// баре фила, засчитывается. Раньше тип входа выводился из равенства цены фила и close,
// и такой фил получал «вход по close» без проверки своего бара (42 ORC-сделки из 1285).
func TestIntrabarFillAtCloseStillChecksSameBarStop(t *testing.T) {
	sess := config.SessionConfig{Timezone: "Europe/Moscow", SessionOpenTime: "10:00", EODCloseTime: "18:40"}
	bar := mskBar(t, "2026-09-01", "11:00", 100)
	bar.Open, bar.High, bar.Low, bar.Close = 100.2, 100.4, 99.6, 100.0
	order := models.Order{Direction: "BUY", Price: 100, StopLoss: 99.7, TakeProfit: 100.37}

	for _, name := range []string{"runner", "portfolio"} {
		t.Run(name+"/лимит", func(t *testing.T) {
			o := order
			o.IntrabarFill = true
			store, open := runBothWith(t, name, sess, []models.Candle{bar}, &fillStrategy{order: &o})
			trades := store.Trades()
			if open || len(trades) != 1 || trades[0].CloseReason != models.CloseReasonStopLoss || trades[0].ExitPrice != 99.7 {
				t.Fatalf("лимитный фил по close 100: стоп 99.7 в баре фила, а получили open=%v %+v", open, trades)
			}
		})
		t.Run(name+"/вход по close", func(t *testing.T) {
			o := order
			store, open := runBothWith(t, name, sess, []models.Candle{bar}, &fillStrategy{order: &o})
			if !open || len(store.Trades()) != 0 {
				t.Fatalf("настоящий вход по close не видит минимума до входа: open=%v сделок=%d", open, len(store.Trades()))
			}
		})
	}
}

// Комиссия при закрытии списывается с кэша исполнителя в обоих backtest, как в live.
// Иначе кэш backtest рос на сумму всех комиссий и кап по кэшу срабатывал не так, как в live:
// объём расходился у 168 сделок из 1535 (TestLiveMatchesBacktestOnHistory).
func TestBacktestCloseChargesCommissionToCash(t *testing.T) {
	sess := config.SessionConfig{Timezone: "Europe/Moscow", SessionOpenTime: "19:05", EODCloseTime: "23:50"}
	candles := []models.Candle{mskBar(t, "2026-09-01", "23:40", 100), mskBar(t, "2026-09-01", "23:45", 100)}
	cc := costs.Config{CommissionRatePerLeg: 0.0008}
	for _, name := range []string{"runner", "portfolio"} {
		t.Run(name, func(t *testing.T) {
			store := memory.NewTradeStore()
			cfg := RunnerConfig{
				Ticker: "SBER", ClassCode: costs.ClassCodeStocks, CandleTimeframe: "M5", StepPriceValue: 1,
				Deposit: 1e6, MaxDailyLoss: 1e6, RiskPerTradePct: 0.1, Strategy: &onceStrategy{},
				SessionCfg: sess, CostsCfg: cc,
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
			} else {
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
			}
			trades := store.Trades()
			if len(trades) != 1 {
				t.Fatalf("ждали одну сделку, получили %d", len(trades))
			}
			tr := trades[0]
			want := 1e6 + tr.GrossPnL - costs.RoundTrip(cc, costs.ClassCodeStocks, tr.EntryPrice, tr.ExitPrice, tr.Quantity, 1)
			bal, _ := exec.GetBalance(context.Background())
			if math.Abs(bal-want) > 1e-6 {
				t.Fatalf("кэш после сделки %.4f, ждали %.4f (депозит + gross − комиссия)", bal, want)
			}
		})
	}
}
