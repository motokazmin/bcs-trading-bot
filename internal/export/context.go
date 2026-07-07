package export

import (
	"fmt"

	"bcs-trading-bot/internal/config"
	"bcs-trading-bot/pkg/models"
)

// StrategyContextFromConfig описывает стратегию для ИИ-анализа.
func StrategyContextFromConfig(cfg *config.Config) models.StrategyContext {
	if cfg == nil {
		return models.StrategyContext{}
	}
	s := cfg.Strategy
	stopMode := s.StopMode
	if stopMode == "" {
		stopMode = "range"
	}

	commission := cfg.CommissionPerLot()
	return models.StrategyContext{
		Name:           s.TypeOrDefault(),
		Philosophy:     "Рынок непредсказуем; управляем риском. Оценка edge по walk-forward backtest на истории.",
		SignalLogic:    fmt.Sprintf("Стратегия %s на M5; stop_mode=%s, lookback=%d.", s.TypeOrDefault(), stopMode, s.Lookback),
		RiskReward:     fmt.Sprintf("ATR/range стоп; reward_ratio=%.2f (эффективный R:R).", s.EffectiveRewardRatio()),
		RiskPerTrade:   fmt.Sprintf("%.2f%% депозита %d ₽ на сделку.", cfg.Risk.RiskPerTradePercent, int(cfg.Risk.Deposit)),
		TrailingStop:   fmt.Sprintf("trail_activation_r=%.2f, trail_stage_max=%d.", s.TrailActivationR, s.TrailStageMax),
		CircuitBreaker: fmt.Sprintf("%.1f%% дневного убытка.", cfg.Risk.MaxDailyLossPercent),
		PnLNote:        fmt.Sprintf("Главная метрика прибыльности — expectancy_r (средний PnL в R на сделку): >0 edge, <0 убыток. PnL в ₽ net (комиссия %.2f ₽ round-trip за единицу quantity). avg_pnl_r = expectancy_r.", commission),
		ExperimentNote: "Optimizer backtest на CSV-истории MOEX; best-config из walk-forward random search.",
	}
}

// DefaultLiveStrategyContext — контекст для paper trading из веб-админки.
func DefaultLiveStrategyContext() models.StrategyContext {
	return models.StrategyContext{
		Name:           "Momentum Breakout (Неидеальный агент)",
		Philosophy:     "Рынок непредсказуем; управляем только риском. Прибыльность при win rate 30–40% за счёт R:R 1:3.",
		SignalLogic:    "Пробой high/low за lookback-1 свечей M5; вход лимитным ордером. В сделке: breakout_upper/breakout_lower — уровни окна на входе.",
		RiskReward:     "Stop-Loss и Take-Profit в соотношении 1:3 (1R риск, 3R цель).",
		RiskPerTrade:   "Размер лота из 0.5% депозита на тикер при срабатывании начального SL.",
		TrailingStop:   "+1R → безубыток; +2R → фиксация +1R; далее SL = MFE − 1R на каждом тике; выход по SL/TP/EOD.",
		CircuitBreaker: "2% дневного убытка на эксперимент → блокировка новых входов до следующего дня.",
		PnLNote:        "gross_pnl в рублях, комиссия брокера не вычтена. Главная метрика — expectancy_r (средний pnl_r на сделку). mfe_in_r / mae_in_r — экскурсии внутри позиции в R.",
		ExperimentNote: "Параллельные experiment_id — разные virtual-счета на одних рыночных данных (stop_mode: range | atr).",
	}
}
