package selfmanaged

import (
	"bcs-trading-bot/internal/engine/trailing"
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"bcs-trading-bot/internal/engine/costs"
	"bcs-trading-bot/internal/engine/contract"
	"bcs-trading-bot/internal/models"
	"bcs-trading-bot/internal/engine/position"
	"bcs-trading-bot/internal/engine/risk"
)

// --- фейки каркаса ---------------------------------------------------------

type recordingTradeStore struct {
	mu       sync.Mutex
	trades   []models.ClosedTrade
	rejected []models.RejectedSignal
}

func (s *recordingTradeStore) SaveClosedTrade(_ context.Context, t models.ClosedTrade) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.trades = append(s.trades, t)
	return nil
}

func (s *recordingTradeStore) SaveRejectedSignal(_ context.Context, r models.RejectedSignal) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rejected = append(s.rejected, r)
	return nil
}

func (s *recordingTradeStore) Rejected() []models.RejectedSignal {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]models.RejectedSignal, len(s.rejected))
	copy(out, s.rejected)
	return out
}

func (s *recordingTradeStore) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.trades)
}

type stubExecutor struct{ calls int }

func (e *stubExecutor) ExecuteOrder(context.Context, models.Order) error { e.calls++; return nil }
func (e *stubExecutor) GetBalance(context.Context) (float64, error)      { return 10_000_000, nil }

type errExecutor struct{ err error }

func (e errExecutor) ExecuteOrder(context.Context, models.Order) error { return e.err }
func (e errExecutor) GetBalance(context.Context) (float64, error)      { return 0, nil }

type fakeRisk struct {
	tryOpenErr error
	opened     map[string]float64
	released   []string
	closed     []string
}

func newFakeRisk() *fakeRisk { return &fakeRisk{opened: map[string]float64{}} }

func (r *fakeRisk) TryOpen(ticker string, amount float64) error {
	if r.tryOpenErr != nil {
		return r.tryOpenErr
	}
	r.opened[ticker] = amount
	return nil
}
func (r *fakeRisk) AdjustOpenRisk(ticker string, amount float64) { r.opened[ticker] = amount }
func (r *fakeRisk) RegisterClose(ticker string, _ float64) {
	r.closed = append(r.closed, ticker)
	delete(r.opened, ticker)
}
func (r *fakeRisk) ReleaseOpen(ticker string) {
	r.released = append(r.released, ticker)
	delete(r.opened, ticker)
}

type fakeCtx struct {
	orders contract.OrderPort
	risk   contract.RiskPort
	trades contract.TradeRecorder
}

func (c *fakeCtx) Ticker() string                 { return "SBER" }
func (c *fakeCtx) Timeframe() string              { return "M5" }
func (c *fakeCtx) Candles() <-chan models.Candle  { return nil }
func (c *fakeCtx) Ticks() <-chan models.Tick      { return nil }
func (c *fakeCtx) Orders() contract.OrderPort     { return c.orders }
func (c *fakeCtx) Risk() contract.RiskPort        { return c.risk }
func (c *fakeCtx) Trades() contract.TradeRecorder { return c.trades }

type fakeClock struct {
	entries    bool
	forceClose bool
	open       bool
}

func (c fakeClock) EntriesAllowed(time.Time) bool   { return c.entries }
func (c fakeClock) ShouldForceClose(time.Time) bool { return c.forceClose }
func (c fakeClock) IsSessionOpen(time.Time) bool    { return c.open }
func (c fakeClock) Today(time.Time) string          { return "2026-06-25" }

type fakeSignal struct{ next *models.Order }

func (s *fakeSignal) ID() string { return "fake" }
func (s *fakeSignal) OnCandle(models.Candle) *models.Order {
	return s.next
}

// --- хелперы -------------------------------------------------------------

func newTestStrategy(cfg Config) *SelfManagedStrategy {
	if cfg.Signal == nil {
		cfg.Signal = &fakeSignal{}
	}
	if cfg.Ticker == "" {
		cfg.Ticker = "SBER"
	}
	if cfg.StepPriceValue == 0 {
		cfg.StepPriceValue = 1.0
	}
	if cfg.Session == nil {
		cfg.Session = fakeClock{entries: true, open: true}
	}
	if cfg.Deposit == 0 {
		cfg.Deposit = 100_000
	}
	if cfg.MaxDailyLoss == 0 {
		cfg.MaxDailyLoss = 2_000
	}
	return New(cfg)
}

// --- тесты (порт из удалённого internal/engine/worker_trade_test.go) -------

func TestClosePositionSavesTrade(t *testing.T) {
	store := &recordingTradeStore{}
	s := newTestStrategy(Config{
		ExperimentID:    "baseline",
		StopMode:        "range",
		TradingMode:     "virtual",
		RunID:           "test-run",
		ClassCode:       "TQBR",
		CandleTimeframe: "M5",
		Lookback:        20,
		RiskPerTradePct: 0.5,
		Deposit:         100_000,
		CostsCfg:        costs.Config{CommissionPerLot: 0.10},
	})
	sctx := &fakeCtx{orders: &stubExecutor{}, risk: newFakeRisk(), trades: store}

	openedAt := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)
	s.pos = &position.State{
		Direction: "BUY", Quantity: 10, EntryPrice: 100,
		InitialStopLoss: 95, InitialTakeProfit: 115,
		StopLoss: 100, TakeProfit: 115, RDistance: 5, TrailStage: 1,
		MFEPrice: 110, BreakoutUpper: 101, BreakoutLower: 99, OpenedAt: openedAt,
	}

	// closePosition получает уже цену исполнения — уровень тейка считает вызывающий.
	s.closePosition(context.Background(), sctx, 115, models.CloseReasonTakeProfit)

	if store.count() != 1 {
		t.Fatalf("trades saved: got %d, want 1", store.count())
	}
	tr := store.trades[0]
	if tr.ExperimentID != "baseline" || tr.StopMode != "range" {
		t.Fatalf("meta: exp=%q stop=%q", tr.ExperimentID, tr.StopMode)
	}
	if tr.ExitPrice != 115 {
		t.Fatalf("exit_price: got %.2f, want 115", tr.ExitPrice)
	}
	if tr.InitialStopLoss != 95 || tr.FinalStopLoss != 100 {
		t.Fatalf("SL: initial=%.2f final=%.2f", tr.InitialStopLoss, tr.FinalStopLoss)
	}
	if tr.CloseReason != models.CloseReasonTakeProfit || tr.TrailStage != 1 {
		t.Fatalf("reason=%q trail=%d", tr.CloseReason, tr.TrailStage)
	}
	if tr.MFEinR != 2 {
		t.Fatalf("mfe_in_r: got %.2f, want 2", tr.MFEinR)
	}
	if s.hasPos() {
		t.Fatal("position must be nil after successful close")
	}
}

func TestClosePositionIgnoresSecondCall(t *testing.T) {
	exec := &stubExecutor{}
	s := newTestStrategy(Config{ExperimentID: "baseline", CostsCfg: costs.Config{CommissionPerLot: 0.10}})
	sctx := &fakeCtx{orders: exec, risk: newFakeRisk(), trades: &recordingTradeStore{}}

	s.pos = &position.State{Direction: "BUY", Quantity: 1, EntryPrice: 100, StopLoss: 95, RDistance: 5, OpenedAt: time.Now()}

	// closePosition получает уже цену исполнения — уровень тейка считает вызывающий.
	s.closePosition(context.Background(), sctx, 115, models.CloseReasonTakeProfit)
	// closePosition получает уже цену исполнения — уровень тейка считает вызывающий.
	s.closePosition(context.Background(), sctx, 115, models.CloseReasonTakeProfit)

	if exec.calls != 1 {
		t.Fatalf("ExecuteOrder calls: got %d, want 1", exec.calls)
	}
	if s.hasPos() {
		t.Fatal("position should stay nil after successful close")
	}
}

func TestClosePositionGhostDropsWithoutRestore(t *testing.T) {
	store := &recordingTradeStore{}
	fr := newFakeRisk()
	fr.opened["CHMF"] = 1000
	s := newTestStrategy(Config{ExperimentID: "or-fade", Ticker: "CHMF"})
	sctx := &fakeCtx{orders: errExecutor{err: contract.ErrNoOpenPosition}, risk: fr, trades: store}

	s.pos = &position.State{Direction: "BUY", Quantity: 1, EntryPrice: 680, StopLoss: 677, TakeProfit: 684, RDistance: 3, OpenedAt: time.Now()}
	s.closePosition(context.Background(), sctx, 677, models.CloseReasonStopLoss)

	if s.hasPos() {
		t.Fatal("ghost close must drop local position (no restore/spam)")
	}
	if len(fr.released) != 1 || fr.released[0] != "CHMF" {
		t.Fatalf("ghost close must release ticker risk, got %v", fr.released)
	}
	if store.count() != 0 {
		t.Fatalf("ghost close must not save trade, got %d", store.count())
	}
}

func TestClosePositionTransientErrorRestores(t *testing.T) {
	store := &recordingTradeStore{}
	fr := newFakeRisk()
	fr.opened["SBER"] = 500
	s := newTestStrategy(Config{ExperimentID: "baseline"})
	sctx := &fakeCtx{orders: errExecutor{err: errors.New("timeout")}, risk: fr, trades: store}

	pos := &position.State{Direction: "BUY", Quantity: 2, EntryPrice: 100, StopLoss: 95, RDistance: 5, OpenedAt: time.Now()}
	s.pos = pos

	s.closePosition(context.Background(), sctx, 95, models.CloseReasonStopLoss)

	if !s.hasPos() {
		t.Fatal("transient executor error must restore position for retry")
	}
	if len(fr.released) != 0 {
		t.Fatalf("transient error must NOT release risk reservation, got %v", fr.released)
	}
	if store.count() != 0 {
		t.Fatalf("failed close must not save trade, got %d", store.count())
	}
}

// Тик, перескочивший стоп, исполняет его по тику, а не по уровню: стоп — рыночная
// заявка, лучше рынка она не исполнится. Раньше бралось уровень, и на гэпах результат
// завышался (docs/analysis/0006).
func TestCheckSLTPStopLossFillsAtWorseOfStopAndTick(t *testing.T) {
	store := &recordingTradeStore{}
	s := newTestStrategy(Config{ExperimentID: "orc-wave2", CandleTimeframe: "M5"})
	sctx := &fakeCtx{orders: &stubExecutor{}, risk: newFakeRisk(), trades: store}

	s.pos = &position.State{
		Direction: "SELL", Quantity: 407, EntryPrice: 484.7,
		InitialStopLoss: 485.84, InitialTakeProfit: 482.83,
		StopLoss: 485.84, TakeProfit: 482.83, RDistance: 1.14,
		MFEPrice: 484.7, MAEPrice: 484.7, OpenedAt: time.Now(),
	}

	s.checkSLTP(context.Background(), sctx, 491.7)

	if store.count() != 1 {
		t.Fatalf("trades: got %d, want 1", store.count())
	}
	tr := store.trades[0]
	if tr.CloseReason != models.CloseReasonStopLoss {
		t.Fatalf("reason: %q", tr.CloseReason)
	}
	if tr.ExitPrice != 491.7 {
		t.Fatalf("exit: got %.2f, want tick 491.7 (стоп 485.84 перескочен)", tr.ExitPrice)
	}
}

// Трейл двигается только по закрытому бару, как в backtest: тик выше активации
// стоп не трогает, закрытый бар — переносит в безубыток. Если новый стоп уже за
// текущей ценой, выход по рынку, а не по уровню (docs/analysis/0006).
func TestTrailMovesOnClosedBarOnly(t *testing.T) {
	store := &recordingTradeStore{}
	s := newTestStrategy(Config{
		ExperimentID: "e", CandleTimeframe: "M5",
		TrailCfg: trailing.Config{ActivationR: 0.5, DiscreteStepR: 1, StageMax: 1, BreakevenR: 0.05, StepPriceValue: 1},
	})
	sctx := &fakeCtx{orders: &stubExecutor{}, risk: newFakeRisk(), trades: store}
	s.pos = &position.State{
		Direction: "BUY", Quantity: 10, EntryPrice: 100, RDistance: 1,
		StopLoss: 99, InitialStopLoss: 99, TakeProfit: 103, InitialTakeProfit: 103,
		MFEPrice: 100, MAEPrice: 100, OpenedAt: time.Now(),
	}
	ctx := context.Background()

	s.setLastPrice(100.8)
	s.checkSLTP(ctx, sctx, 100.8)
	if got := s.pos.StopLoss; got != 99 {
		t.Fatalf("тик не должен двигать трейл, стоп %.4f", got)
	}

	s.setLastPrice(99.9)
	s.checkSLTP(ctx, sctx, 99.9)
	s.trailOnBar(ctx, sctx, models.Candle{Open: 100.2, High: 100.8, Low: 99.6, Close: 99.9})
	if s.hasPos() {
		t.Fatal("закрытый бар перенёс стоп в безубыток 100.05 выше рынка 99.9 — позиция должна закрыться")
	}
	if store.count() != 1 || store.trades[0].ExitPrice != 99.9 {
		t.Fatalf("выход по рынку 99.9, а не по уровню стопа; trades=%+v", store.trades)
	}
}

// --- новое поведение адаптера -------------------------------------------

func TestProcessCandleRejectsStaleBar(t *testing.T) {
	sig := &fakeSignal{next: &models.Order{Direction: "BUY", Price: 100, StopLoss: 95, TakeProfit: 110}}
	s := newTestStrategy(Config{
		Signal: sig, ExperimentID: "e", CandleTimeframe: "M5",
		Session: fakeClock{entries: true, open: true},
	})
	exec := &stubExecutor{}
	sctx := &fakeCtx{orders: exec, risk: newFakeRisk(), trades: &recordingTradeStore{}}

	now := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)
	stale := models.Candle{Close: 100, Timestamp: now.Add(-30 * time.Minute)} // > 3×M5 = 15m

	s.processCandle(context.Background(), sctx, stale, now)

	if s.hasPos() || exec.calls != 0 {
		t.Fatalf("stale bar must not open a position (pos=%v calls=%d)", s.hasPos(), exec.calls)
	}
}

func TestProcessCandleSetsEntryBar(t *testing.T) {
	sig := &fakeSignal{next: &models.Order{Direction: "BUY", Price: 100, StopLoss: 95, TakeProfit: 130}}
	s := newTestStrategy(Config{
		Signal: sig, ExperimentID: "e", CandleTimeframe: "M5",
		Session: fakeClock{entries: true, open: true},
	})
	sctx := &fakeCtx{orders: &stubExecutor{}, risk: newFakeRisk(), trades: &recordingTradeStore{}}

	now := time.Date(2026, 6, 25, 12, 3, 0, 0, time.UTC)
	bar := models.Candle{Open: 100, High: 101, Low: 99.5, Close: 100.5, Timestamp: now.Add(-1 * time.Minute)}

	s.processCandle(context.Background(), sctx, bar, now)

	if !s.hasPos() {
		t.Fatal("fresh signal bar must open a position")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.pos.EntryBarClose != 100.5 || !s.pos.EntryBarTime.Equal(bar.Timestamp) {
		t.Fatalf("entry bar not recorded: close=%.2f time=%s", s.pos.EntryBarClose, s.pos.EntryBarTime)
	}
}

func TestSnapshotPosition(t *testing.T) {
	s := newTestStrategy(Config{ExperimentID: "exp1", Ticker: "SBER", StepPriceValue: 1})
	if s.SnapshotPosition() != nil {
		t.Fatal("no position → nil snapshot")
	}
	s.pos = &position.State{Direction: "BUY", Quantity: 10, EntryPrice: 100, StopLoss: 95, TakeProfit: 115, RDistance: 5, OpenedAt: time.Now()}
	s.lastPrice = 108

	snap := s.SnapshotPosition()
	if snap == nil || snap.Ticker != "SBER" || snap.ExperimentID != "exp1" {
		t.Fatalf("snapshot meta: %+v", snap)
	}
	if snap.LastPrice != 108 || snap.UnrealizedPnL != 80 {
		t.Fatalf("snapshot pnl: last=%.2f uPnL=%.2f want 108 / 80", snap.LastPrice, snap.UnrealizedPnL)
	}
}

func TestGlobalRiskPortIntegration(t *testing.T) {
	// sanity: реальный контроллер удовлетворяет RiskPort и адаптер с ним работает
	gr := risk.NewGlobalRiskController(200_000, 2.0, 0.5, 4)
	var _ contract.RiskPort = gr
	if err := gr.TryOpen("SBER", 500); err != nil {
		t.Fatalf("TryOpen: %v", err)
	}
	gr.RegisterClose("SBER", 100)
	if gr.OpenPositionCount() != 0 {
		t.Fatalf("open count after close: %d", gr.OpenPositionCount())
	}
}

// Live: лимит, исполнившийся ровно по close бара, проверяет стоп своего бара так же,
// как оба backtest (TestIntrabarFillAtCloseStillChecksSameBarStop).
func TestIntrabarFillAtCloseStillChecksSameBarStopLive(t *testing.T) {
	store := &recordingTradeStore{}
	sig := &fakeSignal{next: &models.Order{Direction: "BUY", Price: 100, StopLoss: 99.7, TakeProfit: 100.37, IntrabarFill: true}}
	s := newTestStrategy(Config{Signal: sig, ExperimentID: "e", CandleTimeframe: "M5", Deposit: 1e6, MaxDailyLoss: 1e6})
	sctx := &fakeCtx{orders: &stubExecutor{}, risk: newFakeRisk(), trades: store}
	now := time.Date(2026, 6, 25, 12, 3, 0, 0, time.UTC)
	bar := models.Candle{Open: 100.2, High: 100.4, Low: 99.6, Close: 100, Timestamp: now.Add(-3 * time.Minute)}
	s.setLastPrice(bar.Close)

	s.processCandle(context.Background(), sctx, bar, now)

	if s.hasPos() || store.count() != 1 || store.trades[0].CloseReason != models.CloseReasonStopLoss {
		t.Fatalf("стоп 99.7 в баре фила: pos=%v trades=%+v", s.hasPos(), store.trades)
	}
}

// Неудачное EOD-закрытие повторяется: транзиентная ошибка возвращает позицию, и дата EOD
// не должна ставиться, иначе позиция остаётся на ночь.
func TestEODRetriesAfterTransientCloseError(t *testing.T) {
	store := &recordingTradeStore{}
	s := newTestStrategy(Config{ExperimentID: "e", Session: fakeClock{forceClose: true, open: true}})
	s.pos = &position.State{Direction: "BUY", Quantity: 1, EntryPrice: 100, StopLoss: 99.7, TakeProfit: 100.6, RDistance: 0.3, OpenedAt: time.Now()}
	ctx, now := context.Background(), time.Now()

	failing := &fakeCtx{orders: errExecutor{err: errors.New("timeout")}, risk: newFakeRisk(), trades: store}
	s.checkEOD(ctx, failing, 100, now)
	if !s.hasPos() {
		t.Fatal("после ошибки исполнителя позиция должна остаться")
	}

	ok := &fakeCtx{orders: &stubExecutor{}, risk: newFakeRisk(), trades: store}
	s.checkEOD(ctx, ok, 100.05, now)
	if s.hasPos() || store.count() != 1 || store.trades[0].CloseReason != models.CloseReasonEOD {
		t.Fatalf("повторный EOD должен закрыть позицию: pos=%v trades=%+v", s.hasPos(), store.trades)
	}
}

// Дубликат бара входа (или старая свеча после реконнекта) не двигает трейл: его high
// случился до входа.
func TestTrailIgnoresEntryBarDuplicate(t *testing.T) {
	store := &recordingTradeStore{}
	s := newTestStrategy(Config{
		ExperimentID: "e", CandleTimeframe: "M5",
		TrailCfg: trailing.Config{ActivationR: 0.5, DiscreteStepR: 1, StageMax: 1, BreakevenR: 0.05, StepPriceValue: 1},
	})
	sctx := &fakeCtx{orders: &stubExecutor{}, risk: newFakeRisk(), trades: store}
	entryBar := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)
	s.pos = &position.State{
		Direction: "BUY", Quantity: 10, EntryPrice: 100, RDistance: 0.3,
		StopLoss: 99.7, InitialStopLoss: 99.7, MFEPrice: 100, MAEPrice: 100,
		OpenedAt: entryBar.Add(5 * time.Minute), EntryBarTime: entryBar,
	}
	dup := models.Candle{Open: 100, High: 101, Low: 99.8, Close: 100, Timestamp: entryBar}
	s.setLastPrice(100)

	s.trailOnBar(context.Background(), sctx, dup)

	if !s.hasPos() || s.pos.StopLoss != 99.7 || s.pos.TrailStage != 0 || s.pos.MFEPrice != 100 {
		t.Fatalf("дубликат бара входа сдвинул позицию: %+v", s.pos)
	}
}

type countingSignal struct{ seen []time.Time }

func (s *countingSignal) ID() string { return "counting" }
func (s *countingSignal) OnCandle(c models.Candle) *models.Order {
	s.seen = append(s.seen, c.Timestamp)
	return nil
}

type chanCtx struct {
	fakeCtx
	candles chan models.Candle
}

func (c *chanCtx) Candles() <-chan models.Candle { return c.candles }

// Прогрев (app.Trader.Warmup) уже показал стратегии бары по последний закрытый; WS после
// подписки может прислать тот же бар ещё раз — второй раз в буфер стратегии он не попадает.
func TestБарИзПрогреваНеПовторяетсяИзWS(t *testing.T) {
	sig := &countingSignal{}
	s := newTestStrategy(Config{Signal: sig, ExperimentID: "e", CandleTimeframe: "M5"})
	now := time.Now().UTC().Truncate(5 * time.Minute)
	warmed, next := now.Add(-5*time.Minute), now
	s.SkipBarsThrough(warmed)

	sctx := &chanCtx{
		fakeCtx: fakeCtx{orders: &stubExecutor{}, risk: newFakeRisk(), trades: &recordingTradeStore{}},
		candles: make(chan models.Candle, 2),
	}
	sctx.candles <- models.Candle{Close: 100, Timestamp: warmed}
	sctx.candles <- models.Candle{Close: 100, Timestamp: next}
	close(sctx.candles)
	s.Run(context.Background(), sctx)

	if len(sig.seen) != 1 || !sig.seen[0].Equal(next) {
		t.Fatalf("стратегия получила %v, ждали только %s", sig.seen, next)
	}
}
