package selfmanaged

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"bcs-trading-bot/internal/config"
	"bcs-trading-bot/internal/engine"
	"bcs-trading-bot/internal/engine/execution"
	"bcs-trading-bot/internal/engine/risk"
	"bcs-trading-bot/internal/engine/storage/memory"
	"bcs-trading-bot/internal/models"
	"bcs-trading-bot/internal/optimizer/eval"
)

// TestLiveMatchesBacktestOnHistory гонит data/history через live-методы
// SelfManagedStrategy (processCandle, checkSLTP, trailOnBar, checkEOD) с тем же
// VirtualExecutor и GlobalRiskController, что у backtest, и сверяет сделки поштучно
// с eval.RunPortfolioBacktest. Ловит расхождение трёх путей исполнения на редких
// ветках — границах сессии, пересечении слотов, дневном сбросе (docs/analysis/0007).
//
// Долгий (~2 мин) и требует истории, поэтому только по флагу:
//
//	LIVE_REPLAY=1 go test ./internal/strategy/selfmanaged/ -run TestLiveMatchesBacktestOnHistory -v -timeout 30m
//
// LIVE_REPLAY_CFG — конфиг (по умолчанию configs/runs/portfolio-paper.yaml).
//
// Внутри бара тики идут по пути O→худший→лучший→C с интерполяцией (40 шагов на
// отрезок): стоп пересекается шагом, а не по уровню, отсюда разница выхода ~0.01R
// при той же причине. Порядок слотов на тикере — по алфавиту, как в backtest: в live
// приоритет слотов на одном баре недетерминирован.
func TestLiveMatchesBacktestOnHistory(t *testing.T) {
	if os.Getenv("LIVE_REPLAY") == "" {
		t.Skip("долгий прогон истории — LIVE_REPLAY=1")
	}
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	cfgPath, hist := os.Getenv("LIVE_REPLAY_CFG"), "data/history"
	if cfgPath == "" {
		cfgPath = "configs/runs/portfolio-paper.yaml"
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	acc := cfg.AccountRisk()
	deposit := acc.Deposit
	exec := execution.NewVirtualExecutor(deposit)
	global := risk.NewGlobalRiskController(deposit, acc.MaxDailyLossPercent, acc.RiskPerTradePercent, acc.MaxParallelTrades)
	store := memory.NewTradeStore()
	sctx := &fakeCtx{orders: exec, risk: global, trades: store}

	type slot struct {
		s      *SelfManagedStrategy
		ticker string
	}
	byTicker := map[string][]*SelfManagedStrategy{}
	for _, exp := range cfg.ResolvedExperiments() {
		session := cfg.SessionForExperiment(exp)
		for _, tc := range cfg.TickersForExperiment(exp) {
			clock, err := engine.NewSessionClockExt(session.Timezone, session.EODCloseTime, session.SessionOpenTime,
				session.EntryDelayMinutes, session.WeekdaysOnly, session.WeekendOnly)
			if err != nil {
				t.Fatal(err)
			}
			sig, err := exp.Strategy.BuildStrategy(session)
			if err != nil {
				t.Fatal(err)
			}
			step := tc.StepPriceValue
			if step <= 0 {
				step = 1
			}
			s := New(Config{
				Signal: sig, Label: exp.ID + "/" + tc.Symbol, Ticker: tc.Symbol, ExperimentID: exp.ID,
				StopMode: exp.Strategy.StopMode, StepPriceValue: step, TradingMode: "virtual", RunID: "replay",
				ClassCode: cfg.ClassCode, CandleTimeframe: "M5", Lookback: exp.Strategy.Lookback,
				RiskPerTradePct: acc.RiskPerTradePercent, Deposit: deposit, MaxDailyLoss: acc.MaxDailyLoss,
				CashUtilizationPct: acc.EffectiveCashUtilization(),
				TrailCfg:           exp.Strategy.TrailingConfig(step, cfg.CostsConfig(), cfg.ClassCode),
				CostsCfg:           cfg.CostsConfig(), RewardRatio: exp.Strategy.EffectiveRewardRatio(),
				MaxTradesPerDay: exp.Strategy.MaxTradesPerTickerPerDay, Session: clock,
			})
			byTicker[tc.Symbol] = append(byTicker[tc.Symbol], s)
		}
	}
	for tk := range byTicker {
		sl := byTicker[tk]
		sort.Slice(sl, func(i, j int) bool { return sl[i].cfg.Label < sl[j].cfg.Label })
	}
	tickers := cfg.AllTickerSymbols()
	data, err := eval.LoadCandleData(hist, tickers, "M5")
	if err != nil {
		t.Fatal(err)
	}
	type ev struct {
		ticker string
		c      models.Candle
	}
	var evs []ev
	for tk, cs := range data {
		for _, c := range cs {
			c.Ticker = tk
			evs = append(evs, ev{tk, c})
		}
	}
	sort.Slice(evs, func(i, j int) bool {
		if evs[i].c.Timestamp.Equal(evs[j].c.Timestamp) {
			return evs[i].ticker < evs[j].ticker
		}
		return evs[i].c.Timestamp.Before(evs[j].c.Timestamp)
	})
	ctx := context.Background()
	const legSteps = 40
	ticks := func(e ev) {
		c := e.c
		slots := byTicker[e.ticker]
		// globalDailyResetLoop каждого раннера: сброс по сессии слота, идемпотентно по дате.
		for _, s := range slots {
			if s.cfg.Session.IsSessionOpen(c.Timestamp) {
				global.ResetDaily(s.cfg.Session.Today(c.Timestamp))
			}
		}
		dir := "BUY"
		for _, s := range slots {
			if s.pos != nil {
				dir = s.pos.Direction
			}
		}
		adv, fav := c.Low, c.High
		if dir == "SELL" {
			adv, fav = c.High, c.Low
		}
		pts := []float64{c.Open}
		for _, leg := range [][2]float64{{c.Open, adv}, {adv, fav}, {fav, c.Close}} {
			for i := 1; i <= legSteps; i++ {
				pts = append(pts, leg[0]+(leg[1]-leg[0])*float64(i)/float64(legSteps))
			}
		}
		for i, p := range pts {
			now := c.Timestamp.Add(time.Duration(float64(5*time.Minute) * float64(i) / float64(len(pts))))
			for _, s := range slots {
				s.setLastPrice(p)
				s.checkDailyReset(now)
				s.checkSLTP(ctx, sctx, p)
				s.checkEOD(ctx, sctx, p, now)
			}
		}
	}
	candleClose := func(e ev) {
		c := e.c
		now := c.Timestamp.Add(5*time.Minute + time.Second)
		for _, s := range byTicker[e.ticker] {
			s.setLastPrice(c.Close)
			s.checkDailyReset(now)
			s.checkEOD(ctx, sctx, c.Close, now)
			s.trailOnBar(ctx, sctx, c)
			s.processCandle(ctx, sctx, c, now)
		}
	}
	// Как на рынке: внутри бара — тики по всем тикерам, на закрытии — свечи.
	for i := 0; i < len(evs); {
		j := i
		for j < len(evs) && evs[j].c.Timestamp.Equal(evs[i].c.Timestamp) {
			j++
		}
		for _, e := range evs[i:j] {
			ticks(e)
		}
		for _, e := range evs[i:j] {
			candleClose(e)
		}
		i = j
	}
	live := store.Trades()

	bt, err := eval.RunPortfolioBacktest(ctx, eval.PortfolioBacktestOptions{ConfigPath: cfgPath, HistoryDir: hist, SlippageBps: -1})
	if err != nil {
		t.Fatal(err)
	}
	key := func(tr models.ClosedTrade) string {
		return fmt.Sprintf("%s|%s|%s|%s", tr.ExperimentID, tr.Ticker, tr.Direction, tr.EntryBarTime)
	}
	bm := map[string]models.ClosedTrade{}
	for _, tr := range bt.Trades {
		bm[key(tr)] = tr
	}
	lm := map[string]models.ClosedTrade{}
	for _, tr := range live {
		lm[key(tr)] = tr
	}
	var slipSum, slipMax float64
	slipN := 0
	match, sameExit := 0, 0
	var lsum, bsum float64
	diffReason := map[string]int{}
	entryDiff := map[string]int{}
	capDiff := 0
	var examples, entryEx []string
	for k, l := range lm {
		b, ok := bm[k]
		if !ok {
			continue
		}
		match++
		if l.Quantity != b.Quantity {
			capDiff++
		}
		lsum += l.PnLR
		bsum += b.PnLR
		// Ключ — только время и направление входа: одинаковые ключ и выход при разных
		// цене входа, объёме или стопе дают разный R и рубли. Сверять каждое поле.
		near := func(a, b float64) bool { return math.Abs(a-b) <= 1e-9*math.Max(math.Abs(a), math.Abs(b))+1e-12 }
		for _, f := range []struct {
			name string
			ok   bool
		}{
			{"цена входа", near(l.EntryPrice, b.EntryPrice)},
			{"запрошенный объём", l.RequestedQuantity == b.RequestedQuantity},
			// Итоговый объём может разойтись только через кап по кэшу: тики в этом тесте
			// интерполируются, стоп исполняется чуть хуже уровня, и кэш live на 1–3% меньше.
			{"объём без капа", l.Quantity == b.Quantity || l.CashCapped() || b.CashCapped()},
			{"стоп", near(l.InitialStopLoss, b.InitialStopLoss)},
			{"тейк", near(l.InitialTakeProfit, b.InitialTakeProfit)},
			{"R", near(l.RDistance, b.RDistance)},
		} {
			if !f.ok {
				entryDiff[f.name]++
				if len(entryEx) < 8 {
					entryEx = append(entryEx, fmt.Sprintf("  %s %s: live вход %.4f q=%d/%d cash %.0f | bt вход %.4f q=%d/%d cash %.0f",
						k, f.name, l.EntryPrice, l.Quantity, l.RequestedQuantity, l.CashAtOpen, b.EntryPrice, b.Quantity, b.RequestedQuantity, b.CashAtOpen))
				}
			}
		}
		if l.CloseReason == b.CloseReason && l.RDistance > 0 {
			d := math.Abs(l.ExitPrice-b.ExitPrice) / l.RDistance
			slipSum += d
			slipN++
			if d > slipMax {
				slipMax = d
			}
		}
		if l.CloseReason == b.CloseReason && math.Abs(l.ExitPrice-b.ExitPrice) < 1e-6*b.ExitPrice+1e-9 {
			sameExit++
		} else {
			diffReason[l.CloseReason+"→bt:"+b.CloseReason]++
			if len(examples) < 12 {
				examples = append(examples, fmt.Sprintf("  %s live %s %.4f R=%+.2f | bt %s %.4f R=%+.2f closed %s",
					k, l.CloseReason, l.ExitPrice, l.PnLR, b.CloseReason, b.ExitPrice, b.PnLR, b.ClosedAt.Format("01-02 15:04")))
			}
		}
	}
	onlyL, onlyB := map[string]int{}, map[string]int{}
	var exL, exB []string
	for k, l := range lm {
		if _, ok := bm[k]; !ok {
			onlyL[l.ExperimentID]++
			if len(exL) < 8 {
				exL = append(exL, "  "+k+" "+l.CloseReason)
			}
		}
	}
	for k, b := range bm {
		if _, ok := lm[k]; !ok {
			onlyB[b.ExperimentID]++
			if len(exB) < 8 {
				exB = append(exB, "  "+k+" "+b.CloseReason)
			}
		}
	}
	t.Logf("live=%d bt=%d совпало входов=%d (выход тот же %d); только live=%v; только bt=%v", len(live), len(bt.Trades), match, sameExit, onlyL, onlyB)
	t.Logf("сумма R по совпавшим (валовая у bt, чистая у live): live %+.1f bt %+.1f", lsum, bsum)
	t.Logf("расхождения выхода: %v", diffReason)
	t.Logf("|выход live − выход bt| в R при той же причине: среднее %.4f, макс %.4f (n=%d)", slipSum/float64(slipN), slipMax, slipN)
	for _, x := range examples {
		t.Log(x)
	}
	t.Log("только live:")
	for _, x := range exL {
		t.Log(x)
	}
	t.Log("только bt:")
	for _, x := range exB {
		t.Log(x)
	}

	if len(onlyL)+len(onlyB) > 0 || match != len(live) || match != len(bt.Trades) {
		t.Errorf("сделки live и backtest не совпали поштучно: live=%d bt=%d совпало=%d", len(live), len(bt.Trades), match)
	}
	for k, n := range diffReason {
		if !sameReasonKey(k) {
			t.Errorf("разная причина выхода: %s × %d", k, n)
		}
	}
	t.Logf("объём разошёлся через кап по кэшу: %d сделок", capDiff)
	if len(entryDiff) > 0 {
		t.Errorf("совпавшие сделки расходятся во входе: %v", entryDiff)
		for _, x := range entryEx {
			t.Log(x)
		}
	}
	if slipN > 0 && slipSum/float64(slipN) > 0.02 {
		t.Errorf("средняя разница выхода %.4fR при той же причине — больше артефакта интерполяции тиков", slipSum/float64(slipN))
	}
}

// sameReasonKey — ключ diffReason вида "STOP_LOSS→bt:STOP_LOSS" с одинаковой причиной.
func sameReasonKey(k string) bool {
	for i := 0; i+len("→bt:") <= len(k); i++ {
		if k[i:i+len("→bt:")] == "→bt:" {
			return k[:i] == k[i+len("→bt:"):]
		}
	}
	return false
}
