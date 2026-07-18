package marketdata

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"bcs-trading-bot/internal/bcs"
	"bcs-trading-bot/pkg/logx"
	"bcs-trading-bot/pkg/models"
)

// SyncOptions — параметры инкрементальной синхронизации истории.
type SyncOptions struct {
	OutputDir           string
	ClassCode           string
	TimeFrame           string
	InitialHistoryYears int
	Tickers             []string
	Fetch               FetchConfig
	ParallelTickers     int
	Now                 time.Time // для тестов; zero = time.Now()
}

// SyncHistory догружает историю до текущего момента.
// Первый запуск: initial_history_years назад → now.
// Повторный: от последней свечи в CSV → now (пропуск, если уже актуально).
// Ошибка по одному тикеру не прерывает остальные; fail только если не загружен ни один.
func SyncHistory(ctx context.Context, client *bcs.BCSClient, opts SyncOptions) error {
	if opts.OutputDir == "" {
		opts.OutputDir = "data/history"
	}
	if opts.InitialHistoryYears <= 0 {
		opts.InitialHistoryYears = defaultInitialHistoryYears
	}
	fetchCfg := opts.Fetch.Normalized()
	fetchCfg.Throttle = NewAdaptiveThrottle(fetchCfg.ChunkDelay, fetchCfg.MaxChunkDelay)

	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}
	barDur := barDurationForTimeFrame(opts.TimeFrame)

	var errs []error
	recordErr := func(err error) {
		if err != nil {
			errs = append(errs, err)
			logx.Error("sync-history: %v", err)
		}
	}

	if opts.ParallelTickers <= 1 {
		for i, ticker := range opts.Tickers {
			if i > 0 {
				if err := sleepCtx(ctx, fetchCfg.TickerDelay); err != nil {
					return err
				}
			}
			recordErr(syncOneTicker(ctx, client, opts, fetchCfg, ticker, now, barDur))
		}
		return finishSyncHistory(errs, len(opts.Tickers))
	}

	sem := make(chan struct{}, opts.ParallelTickers)
	var wg sync.WaitGroup
	var errMu sync.Mutex

	for _, ticker := range opts.Tickers {
		wg.Add(1)
		go func(ticker string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			if err := syncOneTicker(ctx, client, opts, fetchCfg, ticker, now, barDur); err != nil {
				errMu.Lock()
				errs = append(errs, err)
				logx.Error("sync-history: %v", err)
				errMu.Unlock()
			}
		}(ticker)
	}
	wg.Wait()
	return finishSyncHistory(errs, len(opts.Tickers))
}

func finishSyncHistory(errs []error, total int) error {
	if len(errs) == 0 {
		return nil
	}
	failed := tickerNamesFromErrors(errs)
	logx.Warn("sync-history: ошибки по %d/%d тикерам: %s", len(errs), total, strings.Join(failed, ", "))
	if len(errs) >= total {
		return fmt.Errorf("ни один тикер не загружен (%s)", strings.Join(failed, ", "))
	}
	return nil
}

func tickerNamesFromErrors(errs []error) []string {
	out := make([]string, 0, len(errs))
	for _, err := range errs {
		if err == nil {
			continue
		}
		msg := err.Error()
		if i := strings.Index(msg, ":"); i > 0 {
			out = append(out, strings.TrimSpace(msg[:i]))
			continue
		}
		out = append(out, msg)
	}
	return out
}

func syncOneTicker(ctx context.Context, client *bcs.BCSClient, opts SyncOptions, fetchCfg FetchConfig, ticker string, now time.Time, barDur time.Duration) error {
	path := filepath.Join(opts.OutputDir, ticker+".csv")
	existing, err := TryLoadCSV(path, ticker)
	if err != nil {
		return fmt.Errorf("%s: чтение CSV: %w", ticker, err)
	}

	from, mode := syncFrom(existing, now, opts.InitialHistoryYears, barDur)
	if mode == syncSkip {
		logx.Info("[%s] история актуальна (последняя свеча %s)", ticker, existing[len(existing)-1].Timestamp.Format(time.RFC3339))
		return nil
	}

	logx.Info("[%s] %s: загрузка %s → %s", ticker, mode, from.Format("2006-01-02"), now.Format("2006-01-02"))
	added, err := fetchTickerRange(ctx, client, path, ticker, opts.ClassCode, opts.TimeFrame, existing, from, now, fetchCfg)
	if err != nil {
		return fmt.Errorf("%s: %w", ticker, err)
	}
	if added == 0 {
		if len(existing) > 0 {
			logx.Warn("[%s] новых свечей не получено (%s → %s) — проверьте доступность данных в API",
				ticker, from.Format("2006-01-02"), now.Format("2006-01-02"))
			return nil
		}
		return fmt.Errorf("%s: данные не получены (%s → %s)", ticker, from.Format("2006-01-02"), now.Format("2006-01-02"))
	}

	total := len(existing) + added
	logx.Info("[%s] сохранено %d свечей (+ %d новых) → %s", ticker, total, added, path)
	return nil
}

type syncMode string

const (
	syncInitial     syncMode = "первичная загрузка"
	syncIncremental syncMode = "догрузка"
	syncSkip        syncMode = "актуально"
)

func syncFrom(existing []models.Candle, now time.Time, initialYears int, barDur time.Duration) (time.Time, syncMode) {
	if len(existing) == 0 {
		return now.AddDate(-initialYears, 0, 0), syncInitial
	}
	last := existing[len(existing)-1].Timestamp
	from := last.Add(barDur)
	if !from.Before(now) {
		return time.Time{}, syncSkip
	}
	return from, syncIncremental
}
