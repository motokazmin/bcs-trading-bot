package app

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

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

	logStartupSummary(opts, cfg)
	warnRefreshTokenExpiry(token)

	logx.Info("Шаг 1: Авторизация через БКС OAuth...")
	if err := client.Connect(ctx); err != nil {
		logx.Fatalf("авторизация провалена: %v", err)
	}
	logx.Info("Access Token получен.")
	return client
}

// warnRefreshTokenExpiry — refresh token БКС живёт 90 дней, и когда он
// истекает, открытый WS торгует дальше, а админка («День», «Стратегии») и любое
// переподключение падают с "Token is not active". Предупреждаем заранее.
func warnRefreshTokenExpiry(token string) {
	exp, ok := broker.RefreshTokenExpiry(token)
	if !ok {
		logx.Warn("не удалось прочитать срок действия BCS_REFRESH_TOKEN")
		return
	}
	left := time.Until(exp)
	switch {
	case left <= 0:
		logx.Warn("BCS_REFRESH_TOKEN истёк %s — выпустите новый в кабинете БКС", exp.Format("2006-01-02 15:04 MST"))
	case left < 14*24*time.Hour:
		logx.Warn("BCS_REFRESH_TOKEN истекает %s (через %d дн.) — выпустите новый заранее", exp.Format("2006-01-02 15:04 MST"), int(left.Hours()/24))
	default:
		logx.Info("BCS_REFRESH_TOKEN действует до %s", exp.Format("2006-01-02"))
	}
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
