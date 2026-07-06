package optimizer

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"bcs-trading-bot/internal/config"
	"bcs-trading-bot/internal/strategy"
	"bcs-trading-bot/internal/trailing"
	"bcs-trading-bot/pkg/logx"
	"bcs-trading-bot/pkg/models"
)

// RunSettings — общие настройки прогона оптимизатора.
type RunSettings struct {
	Tickers            []string
	HistoryDir         string
	StrategyID         string
	StopMode           string
	ClassCode          string
	CandleTimeframe    string
	Deposit            float64
	StepPriceValue     float64
	CommissionPerTrade float64
	MinTrades          int
	Session            config.SessionConfig
}

// Evaluator запускает backtest для набора параметров.
type Evaluator struct {
	settings     RunSettings
	strategyID   string
	buildCtx     strategy.BuildContext
	candleData   map[string][]models.Candle
	windowSlices []WindowCandleSlices
	space        *SearchSpace
}

// NewEvaluator создаёт evaluator с предзагруженной историей.
func NewEvaluator(settings RunSettings, space *SearchSpace, candleData map[string][]models.Candle) *Evaluator {
	sid := strategy.ResolveType(settings.StrategyID)
	if space != nil && space.Strategy != "" {
		sid = strategy.ResolveType(space.Strategy)
	}
	return &Evaluator{
		settings:   settings,
		strategyID: sid,
		buildCtx: strategy.BuildContext{
			StopMode: settings.StopMode,
			Session: strategy.SessionTimes{
				Timezone:          settings.Session.Timezone,
				SessionOpenTime:   settings.Session.SessionOpenTime,
				EntryDelayMinutes: settings.Session.EntryDelayMinutes,
			},
		},
		candleData: candleData,
		space:      space,
	}
}

// EvaluatePeriod прогоняет backtest на заданном периоде для всех тикеров.
func (e *Evaluator) EvaluatePeriod(ctx context.Context, params ParameterSet, from, to time.Time) Metrics {
	byTicker := make(map[string][]models.Candle, len(e.settings.Tickers))
	for _, ticker := range e.settings.Tickers {
		candles, ok := e.candleData[ticker]
		if !ok {
			continue
		}
		filtered := FilterCandles(candles, from, to)
		if len(filtered) > 0 {
			byTicker[ticker] = filtered
		}
	}
	return e.evaluateCandles(ctx, e.newTrialContext(params), byTicker)
}

// AggregateTrades сортирует сделки по времени закрытия и считает Metrics
// по всему портфелю. Сортировка обязательна: сделки по разным тикерам
// закрываются в частично перекрывающиеся моменты времени, а собираются
// они тикер за тикером (сначала все сделки SBER, потом все сделки GAZP,
// и т.д.). Без сортировки по ClosedAt equity curve не отражает реальный
// порядок закрытия позиций на уровне портфеля — MaxDrawdown/Calmar
// считались бы по бессмысленной "сначала весь SBER, потом весь GAZP"
// последовательности вместо настоящей хронологии.
func AggregateTrades(trades []models.ClosedTrade, commissionPerTrade float64) Metrics {
	sorted := make([]models.ClosedTrade, len(trades))
	copy(sorted, trades)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].ClosedAt.Before(sorted[j].ClosedAt)
	})

	var netPnLs []float64
	var returns []float64
	for _, trade := range sorted {
		net := NetPnLFromGross(trade.GrossPnL, trade.Quantity, commissionPerTrade)
		netPnLs = append(netPnLs, net)
		if trade.PnLR != 0 {
			returns = append(returns, trade.PnLR)
		} else {
			riskAmt := trade.RDistance * float64(trade.Quantity) * trade.StepPriceValue
			if riskAmt > 0 {
				returns = append(returns, net/riskAmt)
			}
		}
	}

	return ComputeMetrics(netPnLs, returns)
}

func (e *Evaluator) trailCfg(params ParameterSet) trailing.Config {
	cfg := trailing.DefaultConfig()
	cfg.StepPriceValue = e.settings.StepPriceValue
	cfg.CommissionPerLot = e.settings.CommissionPerTrade
	if v := params.FloatParam("trailActivationR"); v > 0 {
		cfg.ActivationR = v
	}
	if v := params.FloatParam("trailDiscreteStepR"); v > 0 {
		cfg.DiscreteStepR = v
	}
	if v := params.IntParam("trailStageMax"); v > 0 {
		cfg.StageMax = v
	}
	return cfg
}

// LoadCandleData загружает CSV-историю для списка тикеров.
// Тикеры без файла или с пустым CSV пропускаются (WARN в лог).
func LoadCandleData(historyDir string, tickers []string) (map[string][]models.Candle, error) {
	out := make(map[string][]models.Candle, len(tickers))
	var skipped []string
	for _, ticker := range tickers {
		path := filepath.Join(historyDir, ticker+".csv")
		candles, err := TryLoadCSV(path, ticker)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", ticker, err)
		}
		if len(candles) == 0 {
			skipped = append(skipped, ticker)
			continue
		}
		out[ticker] = candles
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("нет загруженной истории ни по одному тикеру (запустите: optimizer sync-history)")
	}
	if len(skipped) > 0 {
		logx.Warn("история пропущена (нет данных): %s", strings.Join(skipped, ", "))
	}
	return out, nil
}
