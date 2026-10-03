package backtest

import (
	"context"
	"testing"
	"time"

	"bcs-trading-bot/internal/engine"
	"bcs-trading-bot/internal/engine/position"
	"bcs-trading-bot/internal/engine/risk"
	"bcs-trading-bot/internal/engine/storage/memory"
	"bcs-trading-bot/internal/engine/trailing"
	"bcs-trading-bot/internal/models"
)

type nopExecutor struct{}

func (nopExecutor) ExecuteOrder(context.Context, models.Order) error { return nil }
func (nopExecutor) GetBalance(context.Context) (float64, error)      { return 1e9, nil }

// intrabarDriver — один из двух путей backtest, которые обязаны вести себя одинаково.
type intrabarDriver struct {
	process func(models.Candle)
	pos     func() *position.State
}

func newIntrabarDrivers(t *testing.T, pos func() *position.State, store *memory.TradeStore) map[string]intrabarDriver {
	t.Helper()
	clock, err := engine.NewSessionClockExt("Europe/Moscow", "18:40", "10:00", 0, false, false)
	if err != nil {
		t.Fatal(err)
	}
	cfg := RunnerConfig{
		Ticker: "SBER", StepPriceValue: 1,
		TrailCfg: trailing.Config{ActivationR: 0.5, DiscreteStepR: 1, StageMax: 1, BreakevenR: 0.05, StepPriceValue: 1},
	}
	ctx := context.Background()

	r := &Runner{cfg: cfg, session: clock, riskMgr: risk.NewRiskManager(1e6, 1e6, 1, 1), store: store, position: pos()}
	st := &tickerState{cfg: cfg, session: clock, riskMgr: risk.NewRiskManager(1e6, 1e6, 1, 1), position: pos()}
	p := &PortfolioRunner{store: store}

	return map[string]intrabarDriver{
		"runner": {
			process: func(c models.Candle) { r.processIntrabar(ctx, nopExecutor{}, c) },
			pos:     func() *position.State { return r.position },
		},
		"portfolio": {
			process: func(c models.Candle) { p.processIntrabar(ctx, nopExecutor{}, st, c) },
			pos:     func() *position.State { return st.position },
		},
	}
}

// Бар дошёл до активации трейла (H=100.8), откатился ниже нового безубытка и
// закрылся на 99.9. Раньше трейл двигался внутри бара по пути O→L→H→C, и позиция
// закрывалась тут же по безубытку 100.05 — модель «знала», что минимум был до
// максимума. Теперь трейл пересчитывается по закрытому бару: в этом баре выхода нет,
// стоп переезжает в безубыток, и следующий бар, открывшийся ниже стопа, исполняет
// его по open (99.9), а не по уровню. Оба перекоса давали +0.05R на мартингале
// (docs/analysis/0006).
func TestTrailMovesOnBarCloseAndGapFillsAtOpen(t *testing.T) {
	t0 := time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)
	newPos := func() *position.State {
		return &position.State{
			Direction: "BUY", Quantity: 10, EntryPrice: 100, RDistance: 1,
			StopLoss: 99, InitialStopLoss: 99, TakeProfit: 103, InitialTakeProfit: 103,
			MFEPrice: 100, MAEPrice: 100, OpenedAt: t0,
		}
	}
	for _, name := range []string{"runner", "portfolio"} {
		t.Run(name, func(t *testing.T) {
			store := memory.NewTradeStore()
			d := newIntrabarDrivers(t, newPos, store)[name]

			d.process(models.Candle{Open: 100.2, High: 100.8, Low: 99.6, Close: 99.9, Timestamp: t0.Add(5 * time.Minute)})
			if d.pos() == nil {
				t.Fatal("трейл не должен закрывать позицию внутри бара, который его взвёл")
			}
			if got := d.pos().StopLoss; got < 100.04 || got > 100.06 {
				t.Fatalf("после бара стоп должен быть в безубытке 100.05, got %.4f", got)
			}

			d.process(models.Candle{Open: 99.9, High: 100.1, Low: 99.8, Close: 100, Timestamp: t0.Add(10 * time.Minute)})
			if d.pos() != nil {
				t.Fatal("бар открылся ниже стопа — позиция должна закрыться")
			}
			trades := store.Trades()
			if len(trades) != 1 {
				t.Fatalf("сделок: %d, want 1", len(trades))
			}
			if got := trades[0].ExitPrice; got != 99.9 {
				t.Fatalf("стоп на гэпе исполняется по open 99.9, а не по уровню 100.05; got %.4f", got)
			}
		})
	}
}
