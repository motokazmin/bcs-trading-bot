// Package notify — тревоги в Telegram из бота. Тот же канал, что у
// scripts/notify.sh (сбои служб): TG_BOT_TOKEN и TG_CHAT_ID приходят из
// /etc/trading-bot/env через EnvironmentFile юнита.
package notify

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"time"

	"bcs-trading-bot/internal/engine/contract"
	"bcs-trading-bot/internal/logx"
)

var _ contract.Alerter = (*Telegram)(nil)

const sendTimeout = 20 * time.Second

// Telegram шлёт тревогу в чат. Без токена или чата только пишет в лог:
// отсутствие канала не должно ронять бота (локальный запуск, smoke).
type Telegram struct {
	token, chat, host string
	apiURL            string
	httpClient        *http.Client
}

// FromEnv — Telegram из TG_BOT_TOKEN/TG_CHAT_ID.
func FromEnv() *Telegram {
	host, _ := os.Hostname()
	t := &Telegram{
		token:      os.Getenv("TG_BOT_TOKEN"),
		chat:       os.Getenv("TG_CHAT_ID"),
		host:       host,
		apiURL:     "https://api.telegram.org",
		httpClient: &http.Client{Timeout: sendTimeout},
	}
	if !t.configured() {
		logx.Warn("Telegram не настроен (TG_BOT_TOKEN, TG_CHAT_ID) — тревоги только в лог")
	}
	return t
}

func (t *Telegram) configured() bool { return t.token != "" && t.chat != "" }

// Alert пишет тревогу в лог и отправляет в фоне.
func (t *Telegram) Alert(msg string) {
	logx.Warn("ТРЕВОГА: %s", msg)
	if !t.configured() {
		return
	}
	go func() {
		if err := t.send(msg); err != nil {
			logx.Error("Telegram: тревога не отправлена: %v", err)
		}
	}()
}

func (t *Telegram) send(msg string) error {
	ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
	defer cancel()
	form := url.Values{"chat_id": {t.chat}, "text": {fmt.Sprintf("[%s] бот: %s", t.host, msg)}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		t.apiURL+"/bot"+t.token+"/sendMessage", nil)
	if err != nil {
		return err
	}
	req.URL.RawQuery = form.Encode()
	resp, err := t.httpClient.Do(req)
	if err != nil {
		// Ошибка net/http содержит URL, а в нём токен бота — в лог не тащим.
		return fmt.Errorf("запрос к api.telegram.org не прошёл")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Telegram ответил %d", resp.StatusCode)
	}
	return nil
}
