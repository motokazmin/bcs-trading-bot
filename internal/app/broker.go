package app

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"bcs-trading-bot/internal/config"
	"bcs-trading-bot/internal/engine/broker"
	"bcs-trading-bot/internal/engine/contract"
	"bcs-trading-bot/internal/engine/notify"
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

	alerter := notify.FromEnv()
	client := broker.NewBCSClient(token)
	client.SetClassCode(cfg.ClassCode)
	client.SetAlerter(alerter)

	logStartupSummary(opts, cfg)
	go watchRefreshTokenExpiry(ctx, token, alerter)

	logx.Info("Шаг 1: Авторизация через БКС OAuth...")
	if err := client.Connect(ctx); err != nil {
		logx.Fatalf("авторизация провалена: %v", err)
	}
	logx.Info("Access Token получен.")
	return client
}

// refreshTokenWarnBefore — за сколько до истечения refresh token звать человека.
const refreshTokenWarnBefore = 14 * 24 * time.Hour

// watchRefreshTokenExpiry — refresh token БКС живёт 90 дней. Истёк — открытый
// WS торгует до первого обрыва, а дальше бот слеп: 2026-10-11 так ушло ~32 ч
// без данных. Проверяем при старте и раз в сутки, за 14 дней шлём в Telegram.
func watchRefreshTokenExpiry(ctx context.Context, token string, alerter contract.Alerter) {
	exp, ok := broker.RefreshTokenExpiry(token)
	if !ok {
		alerter.Alert("не удалось прочитать срок действия BCS_REFRESH_TOKEN — проверить токен")
		return
	}
	logx.Info("BCS_REFRESH_TOKEN действует до %s", exp.Format("2006-01-02 15:04 MST"))
	tick := time.NewTicker(24 * time.Hour)
	defer tick.Stop()
	for {
		if msg, due := refreshTokenExpiryAlert(exp, time.Now()); due {
			alerter.Alert(msg)
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

func refreshTokenExpiryAlert(exp, now time.Time) (string, bool) {
	left := exp.Sub(now)
	switch {
	case left <= 0:
		return fmt.Sprintf("BCS_REFRESH_TOKEN истёк %s — выпустить новый в кабинете БКС, прописать в /etc/trading-bot/env, systemctl restart trading-bot",
			exp.Format("2006-01-02 15:04 MST")), true
	case left < refreshTokenWarnBefore:
		return fmt.Sprintf("BCS_REFRESH_TOKEN истекает %s (через %d дн.) — выпустить новый заранее, прописать в /etc/trading-bot/env, systemctl restart trading-bot",
			exp.Format("2006-01-02 15:04 MST"), int(left.Hours()/24)), true
	}
	return "", false
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
