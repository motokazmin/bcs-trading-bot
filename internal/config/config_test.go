package config_test

import (
	"fmt"
	"testing"

	"bcs-trading-bot/internal/config"
)

func TestLoadPaperM15(t *testing.T) {
	cfg, err := config.Load("../../configs/runs/paper-m15.yaml")
	if err != nil {
		t.Fatal(err)
	}
	exps := cfg.ResolvedExperiments()
	if len(exps) != 2 {
		t.Fatalf("experiments: got %d, want 2", len(exps))
	}
	for _, e := range exps {
		if e.CandleTimeframe != "M15" {
			t.Fatalf("%s: таймфрейм %q, want M15 — бот запускается только на M15", e.ID, e.CandleTimeframe)
		}
	}
	if cfg.AccountRisk().Deposit != 200_000 {
		t.Fatalf("account deposit: got %.0f, want 200000", cfg.AccountRisk().Deposit)
	}
}

func TestLoadRealStocks(t *testing.T) {
	cfg, err := config.Load("../../configs/runs/real-stocks.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.TradingMode != config.TradingModeReal {
		t.Fatalf("trading_mode: got %q", cfg.TradingMode)
	}
	if len(cfg.Tickers) != 1 || cfg.Tickers[0].Symbol != "SBER" {
		t.Fatalf("tickers: %v", cfg.Tickers)
	}
}

func TestLoadFuturesStepPriceValue(t *testing.T) {
	cfg, err := config.Load("../../configs/runs/virtual-futures.yaml")
	if err != nil {
		t.Fatal(err)
	}

	if len(cfg.Tickers) != 2 {
		t.Fatalf("expected 2 tickers, got %d", len(cfg.Tickers))
	}
	if cfg.Tickers[0].Symbol != "SRH6" || cfg.Tickers[0].StepPriceValue != 1.2 {
		t.Fatalf("SRH6: got %+v", cfg.Tickers[0])
	}
	if cfg.Tickers[1].Symbol != "GAZR" || cfg.Tickers[1].StepPriceValue != 1.0 {
		t.Fatalf("GAZR: got %+v", cfg.Tickers[1])
	}
	if cfg.CommissionPerLot() != 5.0 {
		t.Fatalf("commission_per_lot: got %v, want 5.0", cfg.CommissionPerLot())
	}
}

func TestConfigCommissionDefaultByClassCode(t *testing.T) {
	const yamlData = `
trading_mode: virtual
tickers: [SBER]
class_code: TQBR
risk:
  deposit: 100000
  max_daily_loss_percent: 2
`
	cfg, err := config.LoadFromBytes([]byte(yamlData))
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.CommissionPerLot(); got != 0 {
		t.Fatalf("TQBR default flat commission: got %v, want 0 (rate model)", got)
	}
	if !cfg.CostsConfig().UsesRate("TQBR") {
		t.Fatal("expected default rate commission model for TQBR")
	}
}

func TestTickerConfigUnmarshalObject(t *testing.T) {
	const yamlData = `
trading_mode: virtual
tickers:
  - ticker: SRH6
    step_price_value: 2.5
risk:
  deposit: 100000
  max_daily_loss_percent: 2
`

	cfg, err := config.LoadFromBytes([]byte(yamlData))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Tickers[0].Symbol != "SRH6" {
		t.Fatalf("symbol: %q", cfg.Tickers[0].Symbol)
	}
	if cfg.Tickers[0].StepPriceValue != 2.5 {
		t.Fatalf("step_price_value: %f", cfg.Tickers[0].StepPriceValue)
	}
}

// Неизвестный таймфрейм — ошибка загрузки, а не молчаливый M5 (engine.CandleBarDuration):
// с неверной длительностью бара съезжают конец бара, EOD и гейты входа.
func TestUnknownCandleTimeframeRejected(t *testing.T) {
	const base = `
trading_mode: virtual
tickers: [SBER]
session: {eod_close_time: "18:30"}
%s
experiments:
  - id: a
    %s
    strategy:
      type: opening_range_continuation
`
	for _, tc := range []struct{ root, exp string }{
		{"candle_timeframe: M10", ""},
		{"", "candle_timeframe: M10"},
	} {
		if _, err := config.LoadFromBytes([]byte(fmt.Sprintf(base, tc.root, tc.exp))); err == nil {
			t.Fatalf("root=%q exp=%q: ожидалась ошибка", tc.root, tc.exp)
		}
	}
	cfg, err := config.LoadFromBytes([]byte(fmt.Sprintf(base, "candle_timeframe: m15", "")))
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.ResolvedExperiments()[0].CandleTimeframe; got != "M15" {
		t.Fatalf("таймфрейм эксперимента %q, want M15", got)
	}
}

// Время сессии внутри бара live и backtest видят по-разному: на M15 с EOD 18:40 разошлись
// 374 сделки из 886 (0019). Такой конфиг не должен загружаться.
func TestSessionOffGridRejected(t *testing.T) {
	const base = `
trading_mode: virtual
tickers: [SBER]
candle_timeframe: M15
session: {session_open_time: "10:00", eod_close_time: "%s"}
experiments:
  - id: a
    entry_delay_minutes: %d
    strategy:
      type: opening_range_continuation
`
	for _, tc := range []struct {
		eod   string
		delay int
		ok    bool
	}{
		{"18:30", 150, true},
		{"18:40", 150, false}, // EOD внутри бара 18:30–18:45
		{"18:30", 140, false}, // начало входов 12:20 внутри бара
	} {
		_, err := config.LoadFromBytes([]byte(fmt.Sprintf(base, tc.eod, tc.delay)))
		if (err == nil) != tc.ok {
			t.Fatalf("eod=%s delay=%d: err=%v, ожидалось ok=%v", tc.eod, tc.delay, err, tc.ok)
		}
	}
}

// Общие ручки геометрии стопа должны доезжать из YAML до strategy.Params,
// иначе они молча не работают. take_profit_enabled — bool, и без явной
// обработки в factory он терялся бы при конвертации в float-параметры.
func TestCommonStopParamsReachStrategy(t *testing.T) {
	sc := config.StrategyConfigFromFields(map[string]interface{}{
		"type":                "opening_range_continuation",
		"min_stop_bps":        35.0,
		"take_profit_enabled": false,
	}, "atr")

	p, _ := sc.ToParams(config.SessionConfig{
		Timezone: "Europe/Moscow", SessionOpenTime: "10:00",
	})
	if got := p["minStopBps"]; got != 35.0 {
		t.Fatalf("minStopBps: got %v, want 35", got)
	}
	if got, ok := p["takeProfitEnabled"]; !ok || got != 0 {
		t.Fatalf("takeProfitEnabled: got %v (present=%v), want 0", got, ok)
	}
}

// entry_at_close — bool: без строки в switch factory он молча терялся бы (toFloat64 его не берёт).
func TestEntryAtCloseReachesStrategy(t *testing.T) {
	sc := config.StrategyConfigFromFields(map[string]interface{}{
		"type":           "opening_range_continuation",
		"entry_at_close": true,
	}, "atr")
	p, _ := sc.ToParams(config.SessionConfig{Timezone: "Europe/Moscow", SessionOpenTime: "10:00"})
	if got := p["entryAtClose"]; got != 1 {
		t.Fatalf("entryAtClose: got %v, want 1", got)
	}
}
