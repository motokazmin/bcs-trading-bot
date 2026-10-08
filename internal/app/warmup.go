package app

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"bcs-trading-bot/internal/backtest"
	"bcs-trading-bot/internal/config"
	"bcs-trading-bot/internal/engine"
	"bcs-trading-bot/internal/engine/broker"
	"bcs-trading-bot/internal/engine/marketdata"
	"bcs-trading-bot/internal/logx"
	"bcs-trading-bot/internal/models"
	"bcs-trading-bot/internal/strategy"
)

// warmupDays — календарные дни истории для прогрева. Самое длинное окно paper-m15 —
// канал MF (lookback 39) по барам 12:30…18:30, это ~2 торговых дня; 10 — с запасом
// на выходные и праздники.
const warmupDays = 10

// Warmup прогревает стратегии историей до подписки на WS: без него после рестарта
// стратегия копит бары с нуля и день торгует не то, что backtest (старт 2026-10-05:
// у MF пропал вход CHMF, у OR Fade стоп AFKS вышел 22 б.п. вместо 82).
//
// История прогоняется портфельным backtest-раннером через те же экземпляры стратегий,
// что работают в live, — сделки прогрева выбрасываются. Ошибка прогрева не роняет
// бота: тикер стартует холодным, как раньше, и это видно в логе.
func (t *Trader) Warmup(ctx context.Context, client *broker.BCSClient) {
	now := time.Now()
	slots, err := t.warmupSlots()
	if err != nil {
		logx.Error("прогрев: %v — стратегии стартуют без истории", err)
		return
	}

	candles := make(map[string][]models.Candle)
	for ticker, tf := range timeframeByTicker(slots) {
		bars, err := marketdata.FetchCandles(ctx, client, t.cfg.ClassCode, ticker, tf, now.AddDate(0, 0, -warmupDays), now)
		if err != nil {
			logx.Warn("прогрев %s: %v — тикер стартует без истории", ticker, err)
			continue
		}
		if bars = closedBars(bars, tf, now); len(bars) > 0 {
			candles[ticker] = bars
		}
	}
	if len(candles) == 0 {
		logx.Warn("прогрев: истории нет ни по одному тикеру — стратегии стартуют без неё")
		return
	}

	open, err := backtest.Warmup(ctx, slots, candles, portfolioRiskOf(t.cfg).controller())
	if err != nil {
		logx.Error("прогрев: %v — стратегии стартуют без истории", err)
		return
	}

	for ticker, bars := range candles {
		last := bars[len(bars)-1].Timestamp
		for _, s := range t.byTicker[ticker] {
			s.SkipBarsThrough(last)
		}
	}
	logx.Info("Прогрев: %d тикеров, %d слотов, история с %s",
		len(candles), len(slots), now.AddDate(0, 0, -warmupDays).Format("2006-01-02"))
	if len(open) > 0 {
		// Live стартует без позиции и покажет этим слотам бары, которых backtest не показал бы.
		logx.Warn("прогрев: в backtest к старту открыта позиция у %s — до её выхода слоты расходятся с backtest",
			strings.Join(open, ", "))
	}
}

// warmupSlots — RunnerConfig слотов с live-экземплярами стратегий и лимитами live.
func (t *Trader) warmupSlots() (map[string]backtest.RunnerConfig, error) {
	accountRisk := t.cfg.AccountRisk()
	return backtest.SlotConfigs(t.cfg, t.cfg.ResolvedExperiments(), backtest.Account{
		Deposit:            accountRisk.Deposit,
		MaxDailyLoss:       accountRisk.MaxDailyLoss,
		RiskPerTradePct:    accountRisk.RiskPerTradePercent,
		CashUtilizationPct: accountRisk.EffectiveCashUtilization(),
		Costs:              t.cfg.CostsConfig(),
	}, "warmup", func(slotKey string, _ config.ResolvedExperiment, _ config.SessionConfig) (strategy.CandleStrategy, error) {
		s, ok := t.signals[slotKey]
		if !ok {
			return nil, fmt.Errorf("слот %s не собран в BuildTrader", slotKey)
		}
		return s, nil
	})
}

func timeframeByTicker(slots map[string]backtest.RunnerConfig) map[string]string {
	out := make(map[string]string)
	keys := make([]string, 0, len(slots))
	for k := range slots {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		rc := slots[k]
		out[rc.Ticker] = rc.CandleTimeframe
	}
	return out
}

// closedBars отбрасывает формирующийся бар: REST отдаёт и его, а стратегия решает
// только по закрытым (CLAUDE.md, datafeed.Feed.Subscribe).
func closedBars(bars []models.Candle, tf string, now time.Time) []models.Candle {
	barDur := engine.CandleBarDuration(tf)
	n := len(bars)
	for n > 0 && bars[n-1].Timestamp.Add(barDur).After(now) {
		n--
	}
	return bars[:n]
}
