package optimizer

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"bcs-trading-bot/internal/costs"

	"gopkg.in/yaml.v3"
)

const defaultInitialHistoryYears = 2

// UniverseConfig — список инструментов и параметры загрузки истории.
type UniverseConfig struct {
	ClassCode           string       `yaml:"class_code"`
	CandleTimeframe     string       `yaml:"candle_timeframe"`
	InitialHistoryYears int          `yaml:"initial_history_years"`
	Costs               costs.Config `yaml:"costs"`
	LeanTickers         []string     `yaml:"lean_tickers"`
	Tickers             []string     `yaml:"tickers"`
}

// LoadUniverse читает config/optimizer/universe.yaml.
func LoadUniverse(path string) (*UniverseConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("чтение universe %q: %w", path, err)
	}
	var u UniverseConfig
	if err := yaml.Unmarshal(data, &u); err != nil {
		return nil, fmt.Errorf("разбор universe: %w", err)
	}
	u.normalize()
	if len(u.Tickers) == 0 {
		return nil, fmt.Errorf("universe: список tickers пуст")
	}
	return &u, nil
}

func (u *UniverseConfig) normalize() {
	u.ClassCode = strings.TrimSpace(strings.ToUpper(u.ClassCode))
	if u.ClassCode == "" {
		u.ClassCode = "TQBR"
	}
	u.CandleTimeframe = strings.TrimSpace(strings.ToUpper(u.CandleTimeframe))
	if u.CandleTimeframe == "" {
		u.CandleTimeframe = "M5"
	}
	if u.InitialHistoryYears <= 0 {
		u.InitialHistoryYears = defaultInitialHistoryYears
	}
	u.Tickers = normalizeSymbols(u.Tickers)
	u.LeanTickers = normalizeSymbols(u.LeanTickers)
}

// ResolveTickers возвращает список тикеров из universe или явный override (-tickers).
func (u *UniverseConfig) ResolveTickers(override string) []string {
	if override != "" {
		return normalizeSymbols(strings.Split(override, ","))
	}
	return append([]string(nil), u.Tickers...)
}

// CommissionPerLot возвращает комиссию round-trip за единицу quantity.
func (u *UniverseConfig) CommissionPerLot(flagOverride float64) float64 {
	return costs.ResolveFlag(flagOverride, u.ClassCode, u.Costs)
}

func normalizeSymbols(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(strings.ToUpper(s))
		if s == "" {
			continue
		}
		if _, dup := seen[s]; dup {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}
