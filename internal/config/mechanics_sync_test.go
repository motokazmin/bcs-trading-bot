package config_test

import (
	"os"
	"testing"

	"gopkg.in/yaml.v3"
)

// Механика исполнения живёт в четырёх местах, и все обязаны совпадать:
// live (configs/runs/*), чемпионы (configs/champions/*), search space оптимизатора
// (configs/strategies/*) и издержки (configs/shared/tickers*.yaml).
//
// Однажды разошлись первые два — гейт min_stop_bps полтора месяца не работал в live.
// Потом обнаружилось третье и четвёртое: оптимизатор подбирал параметры при
// slippage_bps = 0 (поля просто не было, а дефолт нулевой) и без гейта, то есть
// под сделки, которых бот не возьмёт. Разбор: docs/analysis/0002-*.
func TestИздержкиОбъявленыЯвно(t *testing.T) {
	for _, path := range []string{
		"../../configs/shared/tickers.yaml",
		"../../configs/shared/tickers-orc.yaml",
	} {
		var cfg struct {
			Costs struct {
				SlippageBps *float64 `yaml:"slippage_bps"`
			} `yaml:"costs"`
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		if err := yaml.Unmarshal(raw, &cfg); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		if cfg.Costs.SlippageBps == nil {
			t.Errorf("%s: нет costs.slippage_bps — оптимизатор подберёт параметры "+
				"под идеальный фил (costs.Config.SlippageBps по умолчанию 0)", path)
		}
	}
}

// Search space боевых слотов обязан фиксировать гейт и риск теми же значениями,
// что в configs/runs/portfolio-paper.yaml.
func TestSearchSpaceФиксируетМеханику(t *testing.T) {
	const wantGate, wantRisk = 20.0, 0.20
	for _, path := range []string{
		"../../configs/strategies/orc-research-rolling.yaml",
		"../../configs/strategies/orc-wave2.yaml",
		"../../configs/strategies/or-fade.yaml",
		"../../configs/strategies/or-fade-wave2-narrow.yaml",
		"../../configs/strategies/session-orc-evening.yaml",
		"../../configs/strategies/session-orc-morning.yaml",
		"../../configs/strategies/momentum-filtered-afternoon-longonly-narrow-ws2.yaml",
	} {
		var cfg struct {
			SearchSpace struct {
				Fixed map[string]float64 `yaml:"fixed"`
			} `yaml:"search_space"`
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		if err := yaml.Unmarshal(raw, &cfg); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		got, ok := cfg.SearchSpace.Fixed["minStopBps"]
		if !ok || got != wantGate {
			t.Errorf("%s: fixed.minStopBps = %v (есть=%v), want %v — "+
				"оптимизатор возьмёт сделки, которых live не возьмёт", path, got, ok, wantGate)
		}
		got, ok = cfg.SearchSpace.Fixed["riskPerTradePercent"]
		if !ok || got != wantRisk {
			t.Errorf("%s: fixed.riskPerTradePercent = %v (есть=%v), want %v — "+
				"кап по кэшу сработает иначе, чем в live", path, got, ok, wantRisk)
		}
	}
}
