package app

import (
	"context"
	"fmt"

	"bcs-trading-bot/internal/config"
	"bcs-trading-bot/internal/engine"
	"bcs-trading-bot/internal/engine/broker"
	"bcs-trading-bot/internal/engine/dashboard"
	"bcs-trading-bot/internal/engine/datafeed"
	"bcs-trading-bot/internal/logx"
	"bcs-trading-bot/internal/models"
	"bcs-trading-bot/internal/strategy/selfmanaged"
)

// Trader — собранный торговый набор: по одному StrategyRunner на пару
// (эксперимент × тикер) поверх общего DataFeed и live-Hub.
type Trader struct {
	feed     *datafeed.Feed
	hub      *dashboard.Hub
	runners  []*engine.StrategyRunner
	hubFeeds []hubFeed
	count    int
	expN     int
	eodTime  string
}

// Hub — live-Hub для dashboard.NewServer.
func (t *Trader) Hub() *dashboard.Hub { return t.hub }

// BuildTrader создаёт SessionClock, сигнальную стратегию, SelfManagedStrategy
// и StrategyRunner для каждой пары (эксперимент × тикер), подписывает их на
// DataFeed и регистрирует в live-Hub. При ошибке конфигурации — logx.Fatal.
func BuildTrader(cfg *config.Config, client *broker.BCSClient, deps *Dependencies) *Trader {
	t := &Trader{
		feed:    datafeed.New(client),
		hub:     dashboard.NewHub(),
		expN:    len(cfg.ResolvedExperiments()),
		eodTime: cfg.Session.EODCloseTime,
	}
	hubFeeds := make(map[[2]string]bool) // (ticker, timeframe) → hub уже подписан

	for _, exp := range cfg.ResolvedExperiments() {
		session := cfg.SessionForExperiment(exp)
		timeframe := exp.CandleTimeframe
		if timeframe == "" {
			timeframe = cfg.CandleTimeFrame
		}

		for _, tc := range cfg.TickersForExperiment(exp) {
			// Каждый (эксперимент × тикер) — самодостаточная стратегия
			// (strategy/selfmanaged): сама ведёт SL/TP/трейлинг/EOD/сайзинг.
			// Каркас (StrategyRunner) даёт ей поток данных, OrderExecutor,
			// портфельный риск и TradeStore.
			label := fmt.Sprintf("strategy/%s/%s", exp.ID, tc.Symbol)

			sessionClock, err := engine.NewSessionClockExt(
				session.Timezone, session.EODCloseTime, session.SessionOpenTime,
				session.EntryDelayMinutes, session.WeekdaysOnly, session.WeekendOnly,
			)
			if err != nil {
				logx.Fatalf("ошибка создания SessionClock %s: %v", label, err)
			}

			signalStrategy, err := exp.Strategy.BuildStrategy(session)
			if err != nil {
				logx.Fatalf("ошибка создания сигнальной стратегии %s: %v", label, err)
			}

			step := tc.StepPriceValue
			if step <= 0 {
				step = 1.0
			}
			trailCfg := exp.Strategy.TrailingConfig(step, cfg.CostsConfig(), cfg.ClassCode)

			accountRisk := cfg.AccountRisk()
			selfManaged := selfmanaged.New(selfmanaged.Config{
				Signal:          signalStrategy,
				Label:           label,
				Ticker:          tc.Symbol,
				ExperimentID:    exp.ID,
				StopMode:        exp.Strategy.StopMode,
				StepPriceValue:  step,
				TradingMode:     cfg.TradingMode,
				RunID:           deps.RunID,
				ClassCode:       cfg.ClassCode,
				CandleTimeframe: timeframe,
				Lookback:        exp.Strategy.Lookback,
				RiskPerTradePct:    accountRisk.RiskPerTradePercent,
				Deposit:            accountRisk.Deposit,
				MaxDailyLoss:       accountRisk.MaxDailyLoss,
				CashUtilizationPct: accountRisk.EffectiveCashUtilization(),
				TrailCfg:        trailCfg,
				CostsCfg:        cfg.CostsConfig(),
				RewardRatio:     exp.Strategy.EffectiveRewardRatio(),
				MaxTradesPerDay: exp.Strategy.MaxTradesPerTickerPerDay,
				Session:         sessionClock,
			})
			t.hub.Register(selfManaged)

			candleCh := make(chan models.Candle, 64)
			tickCh := make(chan models.Tick, 256)
			runner := engine.NewStrategyRunner(selfManaged, tc.Symbol, timeframe, candleCh, tickCh, deps.Executor, deps.Portfolio, deps.Store, sessionClock)
			if err := t.feed.Subscribe(tc.Symbol, timeframe, candleCh, tickCh); err != nil {
				logx.Fatalf("ошибка подписки на данные %s (%s): %v", label, timeframe, err)
			}
			t.runners = append(t.runners, runner)
			t.count++

			// live-дашборд: отдельный consumer той же пары (ticker, timeframe)
			// через fan-out DataFeed — свечи/тики в hub для /positions,
			// /candles, /chart. Один на пару, не на каждый эксперимент.
			if key := [2]string{tc.Symbol, timeframe}; !hubFeeds[key] {
				hubFeeds[key] = true
				hubCandleCh := make(chan models.Candle, 128)
				hubTickCh := make(chan models.Tick, 256)
				if err := t.feed.Subscribe(tc.Symbol, timeframe, hubCandleCh, hubTickCh); err != nil {
					logx.Fatalf("ошибка подписки hub %s (%s): %v", label, timeframe, err)
				}
				t.registerHubFeed(tc.Symbol, hubCandleCh, hubTickCh)
			}
		}
	}
	return t
}

// hubFeed — параметры одного отложенного fan-out consumer'а для live-Hub.
type hubFeed struct {
	ticker   string
	candleCh <-chan models.Candle
	tickCh   <-chan models.Tick
}

func (t *Trader) registerHubFeed(ticker string, candleCh <-chan models.Candle, tickCh <-chan models.Tick) {
	t.hubFeeds = append(t.hubFeeds, hubFeed{ticker, candleCh, tickCh})
}

// Run запускает раннеры стратегий, fan-out в live-Hub и стрим DataFeed.
// Блокируется до ctx.Done(); при фатальной ошибке стрима вызывает cancel.
func (t *Trader) Run(ctx context.Context, cancel context.CancelFunc) {
	for _, r := range t.runners {
		go r.Start(ctx)
	}
	for _, hf := range t.hubFeeds {
		go ingestMarketToHub(ctx, t.hub, hf.ticker, hf.candleCh, hf.tickCh)
	}

	logx.Info(
		"Шаг 2: Запущено %d стратегий (%d экспериментов, EOD: %s МСК)",
		t.count, t.expN, t.eodTime,
	)

	go func() {
		if err := t.feed.Run(ctx); err != nil && ctx.Err() == nil {
			logx.Error("стрим рыночных данных остановлен: %v", err)
			cancel() // даём main нормально завершиться через <-ctx.Done()
		}
	}()

	logx.Info("Шаг 3: Торговый цикл активен. Мониторинг SL/TP и EOD включён.")
	<-ctx.Done()
}

// ingestMarketToHub перекладывает свечи/тики из fan-out DataFeed в
// dashboard.Hub (буфер дня + last price для /positions, /candles, /chart).
// Отдельный consumer, не влияет на канал стратегии.
func ingestMarketToHub(
	ctx context.Context,
	hub *dashboard.Hub,
	ticker string,
	candleCh <-chan models.Candle,
	tickCh <-chan models.Tick,
) {
	for {
		select {
		case <-ctx.Done():
			return
		case c, ok := <-candleCh:
			if !ok {
				return
			}
			if c.Ticker == "" {
				c.Ticker = ticker
			}
			hub.IngestCandle(c)
		case tk, ok := <-tickCh:
			if !ok {
				return
			}
			if tk.Ticker == "" {
				tk.Ticker = ticker
			}
			hub.IngestTick(tk)
		}
	}
}
