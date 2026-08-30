package export

import (
	"fmt"

	"bcs-trading-bot/internal/config"
	"bcs-trading-bot/internal/models"
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

	commission := cfg.CostsConfig().Description(cfg.ClassCode)
	trail := fmt.Sprintf("trail_activation_r=%.2f, trail_stage_max=%d", s.TrailActivationR, s.TrailStageMax)
	if s.TrailBreakevenR > 0 {
		trail += fmt.Sprintf(", trail_breakeven_r=%.3f", s.TrailBreakevenR)
	}
	trail += "."

	return models.StrategyContext{
		Name:           s.TypeOrDefault(),
		Philosophy:     "Рынок непредсказуем; управляем риском. Оценка edge по walk-forward backtest на истории. Champions: reward_ratio обычно ~1.2–1.8, не фиксированное 1:3.",
		SignalLogic:    fmt.Sprintf("Стратегия %s на M5; stop_mode=%s, lookback=%d. Вход по закрытию свечи; SL/TP/трейлинг — по котировкам (тики).", s.TypeOrDefault(), stopMode, s.Lookback),
		RiskReward:     fmt.Sprintf("ATR/range стоп; reward_ratio=%.2f (эффективный R:R). Фактический R:R по сделке = |TP−entry|/RDistance.", s.EffectiveRewardRatio()),
		RiskPerTrade:   fmt.Sprintf("%.2f%% депозита %d ₽ на сделку.", cfg.Risk.RiskPerTradePercent, int(cfg.Risk.Deposit)),
		TrailingStop:   trail,
		CircuitBreaker: fmt.Sprintf("%.1f%% дневного убытка на счёт.", cfg.Risk.MaxDailyLossPercent),
		PnLNote:        fmt.Sprintf("Главная метрика — expectancy_r (средний PnL в R на сделку): >0 edge, <0 убыток. PnL в ₽ net (комиссия: %s). avg_pnl_r = expectancy_r.", commission),
		ExperimentNote: "Параллельные experiment_id — слоты стратегий на одном virtual-счёте.",
	}
}

// DefaultLiveStrategyContext — контекст для paper trading из HTTP-админки бота
// (портфель FROZEN champions; per-slot params в YAML, не momentum 1:3).
func DefaultLiveStrategyContext() models.StrategyContext {
	return models.StrategyContext{
		Name:           "Portfolio paper (FROZEN champions)",
		Philosophy:     "Рынок непредсказуем; управляем риском. Edge оценивается по expectancy_r и walk-forward. У champions reward_ratio обычно ~1.2–1.8 — это норма, не баг.",
		SignalLogic:    "Несколько слотов (session ORC morning/evening, ORC, OR Fade, MF Afternoon) на M5. Вход по закрытию свечи; SL/TP/трейлинг — по WebSocket-котировкам (не раз в 5 минут). Холд <5 мин штатен.",
		RiskReward:     "R:R задаётся reward_ratio слота (~1.2–1.8). Фактический R:R по сделке: |InitialTakeProfit−EntryPrice|/RDistance. Не эталон 1:3.",
		RiskPerTrade:   "Размер лота из 0.5% депозита на сделку при срабатывании начального SL; общий virtual-счёт и circuit breaker.",
		TrailingStop:   "Параметры trail_activation_r / trail_breakeven_r / trail_stage_max — per experiment. Если activation дальше TP, trail_stage останется 0 до тейка.",
		CircuitBreaker: "2% дневного убытка на счёт → блокировка новых входов до следующего дня.",
		PnLNote:        "В virtual-режиме бота gross_pnl в БД — уже net (комиссия учтена). Главная метрика — expectancy_r. mfe_in_r / mae_in_r — экскурсии в R. Paper: SL/TP по уровню стопа/тейка; same-bar exit после limit-fill на M5.",
		ExperimentNote: "Параллельные experiment_id — слоты на одном virtual-счёте (разные params / тикеры / сессии). Сравнивай stop_mode только если в данных есть вариативность.",
	}
}
