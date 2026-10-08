package backtest

import (
	"context"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
	"time"

	"bcs-trading-bot/internal/config"
	"bcs-trading-bot/internal/engine/execution"
	"bcs-trading-bot/internal/engine/marketdata"
	"bcs-trading-bot/internal/engine/risk"
	"bcs-trading-bot/internal/engine/storage/memory"
	"bcs-trading-bot/internal/models"
	"bcs-trading-bot/internal/strategy"
)

// После рестарта live стартует с прогретыми стратегиями (app.Trader.Warmup). Прогрев до
// полуночи плюс продолжение обязаны дать те же входы, что непрерывный backtest, а холодный
// старт — другие: иначе прогрев ничего не держит. Стратегии paper-m15 на M5-истории из git.
func TestПрогревДаётТеЖеВходыЧтоНепрерывныйBacktest(t *testing.T) {
	root := "../.."
	cfg, err := config.Load(filepath.Join(root, "configs/runs/paper-m15.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	experiments := cfg.ResolvedExperiments()
	for i := range experiments {
		experiments[i].CandleTimeframe = "M5"
	}
	msk := time.FixedZone("MSK", 3*3600)
	from := time.Date(2026, 8, 1, 0, 0, 0, 0, msk)
	cutoff := time.Date(2026, 9, 15, 0, 0, 0, 0, msk)
	to := time.Date(2026, 10, 3, 0, 0, 0, 0, msk)

	before, after, all := map[string][]models.Candle{}, map[string][]models.Candle{}, map[string][]models.Candle{}
	for _, ticker := range cfg.TickerSymbols() {
		bars, err := marketdata.TryLoadCSV(filepath.Join(root, "data/history", ticker+".csv"), ticker)
		if err != nil || len(bars) == 0 {
			continue // AFKS и др. не в git
		}
		all[ticker] = marketdata.FilterCandles(bars, from, to)
		before[ticker] = marketdata.FilterCandles(bars, from, cutoff)
		after[ticker] = marketdata.FilterCandles(bars, cutoff, to)
	}
	if len(all) < 5 {
		t.Fatalf("истории в data/history: %d тикеров", len(all))
	}

	acc := cfg.AccountRisk()
	newSlots := func() map[string]RunnerConfig {
		slots, err := SlotConfigs(cfg, experiments, Account{
			Deposit: acc.Deposit, MaxDailyLoss: acc.MaxDailyLoss, RiskPerTradePct: acc.RiskPerTradePercent,
			CashUtilizationPct: acc.EffectiveCashUtilization(), Costs: cfg.CostsConfig(),
		}, "test", func(_ string, exp config.ResolvedExperiment, session config.SessionConfig) (strategy.CandleStrategy, error) {
			return exp.Strategy.BuildStrategy(session)
		})
		if err != nil {
			t.Fatal(err)
		}
		return slots
	}
	global := func() *risk.GlobalRiskController {
		return risk.NewGlobalRiskController(acc.Deposit, acc.MaxDailyLossPercent, acc.RiskPerTradePercent, acc.MaxParallelTrades)
	}
	entries := func(slots map[string]RunnerConfig, candles map[string][]models.Candle) []string {
		store := memory.NewTradeStore()
		p, err := NewPortfolioRunner(PortfolioRunnerConfig{Tickers: slots, GlobalRisk: global()}, store)
		if err != nil {
			t.Fatal(err)
		}
		if err := p.Run(context.Background(), candles, execution.NewVirtualExecutor(acc.Deposit)); err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, tr := range store.Trades() {
			if !tr.OpenedAt.Before(cutoff) {
				out = append(out, tr.ExperimentID+"/"+tr.Ticker+" "+tr.Direction+" "+tr.OpenedAt.UTC().Format(time.RFC3339))
			}
		}
		sort.Strings(out)
		return out
	}

	continuous := entries(newSlots(), all)

	warmSlots := newSlots()
	if _, err := Warmup(context.Background(), warmSlots, before, global()); err != nil {
		t.Fatal(err)
	}
	warm := entries(warmSlots, after)

	cold := entries(newSlots(), after)

	if len(continuous) < 20 {
		t.Fatalf("мало сделок для проверки: %d", len(continuous))
	}
	if !reflect.DeepEqual(warm, continuous) {
		t.Fatalf("прогретый старт разошёлся с непрерывным backtest:\nпрогрев  %d %v\nнепрерыв %d %v", len(warm), warm, len(continuous), continuous)
	}
	if reflect.DeepEqual(cold, continuous) {
		t.Fatal("холодный старт совпал с непрерывным — тест не отличает прогрев от его отсутствия")
	}
	t.Logf("входов после %s: непрерывно %d, прогрев %d, холодно %d", cutoff.Format("2006-01-02"), len(continuous), len(warm), len(cold))
}
