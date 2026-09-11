diff --git a/internal/app/trader.go b/internal/app/trader.go
index 50cf4f8..e8ea822 100644
--- a/internal/app/trader.go
+++ b/internal/app/trader.go
@@ -87,9 +87,10 @@ func BuildTrader(cfg *config.Config, client *broker.BCSClient, deps *Dependencie
 				ClassCode:       cfg.ClassCode,
 				CandleTimeframe: timeframe,
 				Lookback:        exp.Strategy.Lookback,
-				RiskPerTradePct: accountRisk.RiskPerTradePercent,
-				Deposit:         accountRisk.Deposit,
-				MaxDailyLoss:    accountRisk.MaxDailyLoss,
+				RiskPerTradePct:    accountRisk.RiskPerTradePercent,
+				Deposit:            accountRisk.Deposit,
+				MaxDailyLoss:       accountRisk.MaxDailyLoss,
+				CashUtilizationPct: accountRisk.EffectiveCashUtilization(),
 				TrailCfg:        trailCfg,
 				CostsCfg:        cfg.CostsConfig(),
 				RewardRatio:     exp.Strategy.EffectiveRewardRatio(),
diff --git a/internal/backtest/portfolio.go b/internal/backtest/portfolio.go
index 0477fb6..f72db4e 100644
--- a/internal/backtest/portfolio.go
+++ b/internal/backtest/portfolio.go
@@ -218,8 +218,14 @@ func (p *PortfolioRunner) processCandle(ctx context.Context, executor contract.O
 	if qty <= 0 {
 		return
 	}
+	entryAtClose := signal.Price == candle.Close
+	// Проскальзывание на входе — см. комментарий в runner.go. Считаем fillPrice
+	// ДО капа по кэшу: иначе для BUY (fill дороже сигнала) notional ордера может
+	// превысить остаток, закэпленный по старой, более низкой цене (см. живой баг,
+	// ADR/фикс от 2026-09-03).
+	fillPrice := costs.FillPrice(st.cfg.CostsCfg, signal.Direction, signal.Price)
 	if bal, err := executor.GetBalance(ctx); err == nil {
-		qty = risk.CapQuantityByCash(qty, signal.Price, bal, st.cfg.StepPriceValue)
+		qty = risk.CapQuantityByCash(qty, fillPrice, bal, st.cfg.StepPriceValue)
 		if qty <= 0 {
 			return
 		}
@@ -229,9 +235,7 @@ func (p *PortfolioRunner) processCandle(ctx context.Context, executor contract.O
 	if signal.Ticker == "" {
 		signal.Ticker = st.cfg.Ticker
 	}
-	entryAtClose := signal.Price == candle.Close
-	// Проскальзывание на входе — см. комментарий в runner.go.
-	signal.Price = costs.FillPrice(st.cfg.CostsCfg, signal.Direction, signal.Price)
+	signal.Price = fillPrice
 
 	tradeRisk := abs(signal.Price-signal.StopLoss) * float64(qty) * st.cfg.StepPriceValue
 	if p.global != nil {
diff --git a/internal/backtest/runner.go b/internal/backtest/runner.go
index 91c9fb3..0086540 100644
--- a/internal/backtest/runner.go
+++ b/internal/backtest/runner.go
@@ -153,19 +153,21 @@ func (r *Runner) processCandle(ctx context.Context, executor contract.OrderExecu
 	if qty <= 0 {
 		return
 	}
+	entryAtClose := signal.Price == candle.Close
+	// Проскальзывание на входе: позиция открывается хуже сигнальной цены.
+	// SL/TP остаются там, где их поставила стратегия, поэтому фактический R
+	// слегка отличается от задуманного — ровно как в реальном исполнении.
+	// Считаем fillPrice ДО капа по кэшу — см. комментарий в portfolio.go.
+	fillPrice := costs.FillPrice(r.cfg.CostsCfg, signal.Direction, signal.Price)
 	if bal, err := executor.GetBalance(ctx); err == nil {
-		qty = risk.CapQuantityByCash(qty, signal.Price, bal, r.cfg.StepPriceValue)
+		qty = risk.CapQuantityByCash(qty, fillPrice, bal, r.cfg.StepPriceValue)
 		if qty <= 0 {
 			return
 		}
 	}
 	signal.Quantity = qty
 	signal.OrderType = models.OrderTypeLimit
-	entryAtClose := signal.Price == candle.Close
-	// Проскальзывание на входе: позиция открывается хуже сигнальной цены.
-	// SL/TP остаются там, где их поставила стратегия, поэтому фактический R
-	// слегка отличается от задуманного — ровно как в реальном исполнении.
-	signal.Price = costs.FillPrice(r.cfg.CostsCfg, signal.Direction, signal.Price)
+	signal.Price = fillPrice
 
 	if err := executor.ExecuteOrder(ctx, *signal); err != nil {
 		return
diff --git a/internal/config/config.go b/internal/config/config.go
index 751be20..e9674ae 100644
--- a/internal/config/config.go
+++ b/internal/config/config.go
@@ -128,6 +128,19 @@ type RiskConfig struct {
 	MaxDailyLossPercent float64 `yaml:"max_daily_loss_percent"`
 	RiskPerTradePercent float64 `yaml:"risk_per_trade_percent"`
 	MaxParallelTrades   int     `yaml:"max_parallel_trades"`
+	// CashUtilizationPercent — сколько от свободного кэша можно резервировать под одну
+	// позицию (0-100]. Буфер защищает от отказов на границе (slippage/комиссия/раунд-off
+	// между капом и фактическим ордером) и оставляет кэш другим параллельным слотам на
+	// общем virtual-счёте. 0 или не задано → дефолт 95.
+	CashUtilizationPercent float64 `yaml:"cash_utilization_percent"`
+}
+
+// EffectiveCashUtilization возвращает долю кэша (0;1], доступную под одну позицию.
+func (r RiskConfig) EffectiveCashUtilization() float64 {
+	if r.CashUtilizationPercent <= 0 || r.CashUtilizationPercent > 100 {
+		return 0.95
+	}
+	return r.CashUtilizationPercent / 100
 }
 
 type VirtualConfig struct {
diff --git a/internal/logx/logx.go b/internal/logx/logx.go
index fc75f1d..0fcf77d 100644
--- a/internal/logx/logx.go
+++ b/internal/logx/logx.go
@@ -205,6 +205,22 @@ func TradeOpen(ticker, direction string, qty int, price, sl, tp float64) {
 	)
 }
 
+// CashCap — кап по кэшу реально урезал объём позиции (даже если сделка всё
+// равно открылась) — сигнал конкуренции за общий virtual-баланс. riskQty —
+// объём, посчитанный риск-моделью; cappedQty — фактический после капа.
+func CashCap(ticker, label string, riskQty, cappedQty int, notional, balance float64) {
+	pct := 0.0
+	if balance > 0 {
+		pct = notional / balance * 100
+	}
+	write(
+		tickerLabel(ticker),
+		tag("CASHCAP", yellow),
+		paint(dim, label),
+		fmt.Sprintf("qty %d→%d notional=%.2f/%.2f (%.1f%%)", riskQty, cappedQty, notional, balance, pct),
+	)
+}
+
 // SignalRejected — сигнал отклонён.
 func SignalRejected(ticker, direction, reason string) {
 	write(
diff --git a/internal/strategy/selfmanaged/selfmanaged.go b/internal/strategy/selfmanaged/selfmanaged.go
index e223aeb..b3c07bf 100644
--- a/internal/strategy/selfmanaged/selfmanaged.go
+++ b/internal/strategy/selfmanaged/selfmanaged.go
@@ -62,6 +62,9 @@ type Config struct {
 	RiskPerTradePct float64
 	Deposit         float64
 	MaxDailyLoss    float64
+	// CashUtilizationPct — доля свободного кэша (0;1], которую можно резервировать
+	// под одну позицию. 0 → дефолт 0.95 (см. config.RiskConfig.EffectiveCashUtilization).
+	CashUtilizationPct float64
 	TrailCfg        trailing.Config
 	CostsCfg        costs.Config
 	RewardRatio     float64
@@ -91,6 +94,9 @@ func New(cfg Config) *SelfManagedStrategy {
 	if cfg.StepPriceValue <= 0 {
 		cfg.StepPriceValue = 1.0
 	}
+	if cfg.CashUtilizationPct <= 0 || cfg.CashUtilizationPct > 1 {
+		cfg.CashUtilizationPct = 0.95
+	}
 	return &SelfManagedStrategy{
 		cfg:     cfg,
 		riskMgr: risk.NewRiskManager(cfg.Deposit, cfg.MaxDailyLoss, cfg.RiskPerTradePct, cfg.StepPriceValue),
@@ -251,15 +257,37 @@ func (s *SelfManagedStrategy) processCandle(ctx context.Context, sctx contract.S
 		logx.SignalRejected(s.cfg.Label, signal.Direction, "нулевой объём позиции")
 		return
 	}
+
+	entryAtClose := signal.Price == candle.Close
+	// Проскальзывание на входе: позиция открывается хуже сигнальной цены.
+	// SL/TP остаются там, где их поставила стратегия.
+	// Считаем fill-цену ДО капа по кэшу: кап и фактический ордер должны
+	// смотреть на одну и ту же цену, иначе для BUY (fill дороже сигнала)
+	// notional ордера может незаметно превысить остаток, закэпленный по
+	// старой, более низкой цене.
+	fillPrice := costs.FillPrice(s.cfg.CostsCfg, signal.Direction, signal.Price)
+
 	if bal, err := sctx.Orders().GetBalance(ctx); err == nil {
-		quantity = risk.CapQuantityByCash(quantity, signal.Price, bal, s.cfg.StepPriceValue)
+		// Резервируем не весь кэш, а долю (см. CashUtilizationPct): запас на
+		// проскальзывание/раунд-off и на параллельные слоты, делящие один счёт.
+		riskQty := quantity
+		quantity = risk.CapQuantityByCash(quantity, fillPrice, bal*s.cfg.CashUtilizationPct, s.cfg.StepPriceValue)
 		if quantity <= 0 {
 			logx.SignalRejected(s.cfg.Label, signal.Direction, "недостаточно средств")
 			return
 		}
+		if quantity < riskQty {
+			// Кап реально сработал — статистика конкуренции за общий virtual-кэш,
+			// даже когда сделка всё же открылась (см. roadmap: потолок notional
+			// на позицию).
+			logx.CashCap(s.cfg.Ticker, s.cfg.Label, riskQty, quantity, fillPrice*float64(quantity)*s.cfg.StepPriceValue, bal)
+		}
 	}
 
-	tradeRisk := math.Abs(signal.Price-signal.StopLoss) * float64(quantity) * s.cfg.StepPriceValue
+	// Риск считаем по fill-цене (после slippage), иначе резерв в GlobalRiskController
+	// систематически расходится с фактическим |fillPrice-SL|: для BUY бюджет
+	// недооценивает риск, для SELL — переоценивает.
+	tradeRisk := math.Abs(fillPrice-signal.StopLoss) * float64(quantity) * s.cfg.StepPriceValue
 	if err := sctx.Risk().TryOpen(s.cfg.Ticker, tradeRisk); err != nil {
 		logx.SignalRejected(s.cfg.Label, signal.Direction, err.Error())
 		return
@@ -268,10 +296,7 @@ func (s *SelfManagedStrategy) processCandle(ctx context.Context, sctx contract.S
 	signal.Quantity = quantity
 	signal.OrderType = models.OrderTypeLimit
 	signal.Ticker = s.cfg.Ticker
-	entryAtClose := signal.Price == candle.Close
-	// Проскальзывание на входе: позиция открывается хуже сигнальной цены.
-	// SL/TP остаются там, где их поставила стратегия.
-	signal.Price = costs.FillPrice(s.cfg.CostsCfg, signal.Direction, signal.Price)
+	signal.Price = fillPrice
 
 	if err := sctx.Orders().ExecuteOrder(ctx, *signal); err != nil {
 		logx.Error("[%s] ошибка открытия позиции: %v", s.cfg.Label, err)
