package app

import (
	"context"
	"net/http"
	"os"
	"strings"
	"time"

	"bcs-trading-bot/internal/config"
	"bcs-trading-bot/internal/engine/api"
	"bcs-trading-bot/internal/engine/broker"
	"bcs-trading-bot/internal/engine/dashboard"
	"bcs-trading-bot/internal/logx"
)

// StartDashboard поднимает HTTP UI/API админки в фоне (если opts.HTTPListen
// не пуст) и вешает graceful-shutdown на ctx. При ошибке конфигурации
// сервера — logx.Fatal.
func StartDashboard(ctx context.Context, opts Options, cfg *config.Config, tr *Trader, deps *Dependencies, client *broker.BCSClient) {
	if opts.HTTPListen == "" {
		return
	}

	adminToken := strings.TrimSpace(os.Getenv("ADMIN_TOKEN"))
	srv, err := dashboard.NewServer(tr.Hub(), dashboard.Options{
		Listen:   opts.HTTPListen,
		Token:    adminToken,
		Deposit:  cfg.AccountBalance(),
		Exec:     deps.Executor,
		Reader:   deps.Reader,
		Archives: api.NewArchiveStore(opts.ArchivesPath),
		Candles:  dashboard.NewCachedDayCandles(&dashboard.BCSCandleFetcher{Client: client}, cfg.ClassCode, 0),
	})
	if err != nil {
		logx.Fatalf("HTTP UI/API: %v", err)
	}

	httpSrv := &http.Server{
		Addr:              srv.ListenAddr(),
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      60 * time.Second,
	}
	go func() {
		authHint := "без токена (только localhost)"
		if adminToken != "" {
			authHint = "ADMIN_TOKEN задан"
		}
		logx.Info("Админка: http://%s (%s)", srv.ListenAddr(), authHint)
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logx.Error("HTTP UI/API: %v", err)
		}
	}()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancelShutdown()
		_ = httpSrv.Shutdown(shutdownCtx)
	}()
}
