package app

import (
	"context"
	"fmt"
	"os"
	"strings"

	"bcs-trading-bot/internal/config"
	"bcs-trading-bot/internal/engine/broker"
	"bcs-trading-bot/internal/logx"
)

// MustConnectBroker создаёт клиента БКС, печатает сводку запуска и проходит
// OAuth. Требует переменную окружения BCS_REFRESH_TOKEN. При любой ошибке —
// logx.Fatal.
func MustConnectBroker(ctx context.Context, opts Options, cfg *config.Config) *broker.BCSClient {
	token := os.Getenv("BCS_REFRESH_TOKEN")
	if token == "" {
		logx.Fatal("задайте переменную окружения BCS_REFRESH_TOKEN")
	}

	client := broker.NewBCSClient(token)
	client.SetClassCode(cfg.ClassCode)
	client.SetCandleTimeFrame(cfg.CandleTimeFrame)

	logStartupSummary(opts, cfg)

	logx.Info("Шаг 1: Авторизация через БКС OAuth...")
	if err := client.Connect(ctx); err != nil {
		logx.Fatalf("авторизация провалена: %v", err)
	}
	logx.Info("Access Token получен.")
	return client
}

func logStartupSummary(opts Options, cfg *config.Config) {
	experiments := cfg.ResolvedExperiments()
	expNames := make([]string, len(experiments))
	for i, exp := range experiments {
		if exp.Name != "" && exp.Name != exp.ID {
			expNames[i] = fmt.Sprintf("%s (%s)", exp.ID, exp.Name)
		} else {
			expNames[i] = exp.ID
		}
	}

	logx.Info(
		"Конфиг: %s | Режим: %s | Тикеры: %s | Эксперименты: %s | Класс: %s | Свечи: %s",
		opts.ConfigPath,
		cfg.TradingMode,
		strings.Join(cfg.AllTickerSymbols(), ", "),
		strings.Join(expNames, ", "),
		cfg.ClassCode,
		cfg.CandleTimeFrame,
	)
}
