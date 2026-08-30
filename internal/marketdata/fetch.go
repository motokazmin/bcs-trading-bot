package marketdata

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"bcs-trading-bot/internal/bcs"
	"bcs-trading-bot/internal/logx"
	"bcs-trading-bot/internal/models"
)

const (
	candlesChartURL = "https://be.broker.ru/trade-api-market-data-connector/api/v1/candles-chart"
	maxBarsPerReq   = 1000
)

var candlesHTTPClient = &http.Client{Timeout: 30 * time.Second}

type candlesResponse struct {
	Bars []candleBar `json:"bars"`
}

type candleBar struct {
	Time   string  `json:"time"`
	Open   float64 `json:"open"`
	High   float64 `json:"high"`
	Low    float64 `json:"low"`
	Close  float64 `json:"close"`
	Volume float64 `json:"volume"`
}

// FetchCandles загружает исторические свечи с пагинацией (до 1000 баров за запрос).
func FetchCandles(ctx context.Context, client *bcs.BCSClient, classCode, ticker, timeFrame string, from, to time.Time) ([]models.Candle, error) {
	return FetchCandlesWithConfig(ctx, client, classCode, ticker, timeFrame, from, to, DefaultFetchConfig())
}

// FetchCandlesWithConfig — FetchCandles с настраиваемым throttling/retry.
func FetchCandlesWithConfig(ctx context.Context, client *bcs.BCSClient, classCode, ticker, timeFrame string, from, to time.Time, cfg FetchConfig) ([]models.Candle, error) {
	if client.AccessToken() == "" {
		return nil, fmt.Errorf("клиент не авторизован")
	}
	cfg = cfg.Normalized()

	var all []models.Candle
	chunkStart := from
	barDuration := barDurationForTimeFrame(timeFrame)

	for chunkStart.Before(to) {
		if !cfg.Adaptive {
			if err := sleepCtx(ctx, cfg.ChunkDelay); err != nil {
				return nil, err
			}
		}

		chunkEnd := chunkStart.Add(barDuration * maxBarsPerReq)
		if chunkEnd.After(to) {
			chunkEnd = to
		}

		bars, err := fetchCandlesChunk(ctx, client, classCode, ticker, timeFrame, chunkStart, chunkEnd, cfg)
		if err != nil {
			return nil, err
		}
		if len(bars) == 0 {
			chunkStart = nextChunkStart(chunkStart, chunkEnd, time.Time{}, barDuration, false)
			continue
		}
		all = append(all, bars...)

		last := lastCandleTS(bars)
		chunkStart = nextChunkStart(chunkStart, chunkEnd, last, barDuration, true)
		if !chunkStart.Before(to) {
			break
		}
	}

	return dedupeCandles(all), nil
}

// fetchTickerRange загружает диапазон чанками с append-checkpoint в CSV после каждого чанка.
func fetchTickerRange(ctx context.Context, client *bcs.BCSClient, path, ticker, classCode, timeFrame string, existing []models.Candle, from, to time.Time, cfg FetchConfig) (int, error) {
	cfg = cfg.Normalized()
	throttle := cfg.ThrottleOrDefault()

	totalCount := len(existing)
	var lastTS time.Time
	if totalCount > 0 {
		lastTS = existing[totalCount-1].Timestamp
	}
	fileExists := totalCount > 0

	barDuration := barDurationForTimeFrame(timeFrame)
	chunkStart := from
	added := 0
	chunkNum := 0

	for chunkStart.Before(to) {
		if !cfg.Adaptive {
			if err := sleepCtx(ctx, cfg.ChunkDelay); err != nil {
				return added, err
			}
		}

		chunkEnd := chunkStart.Add(barDuration * maxBarsPerReq)
		if chunkEnd.After(to) {
			chunkEnd = to
		}

		bars, err := fetchCandlesChunk(ctx, client, classCode, ticker, timeFrame, chunkStart, chunkEnd, cfg)
		if err != nil {
			return added, err
		}
		if len(bars) == 0 {
			logx.Info("[%s] пустой ответ API %s → %s, пропуск окна",
				ticker, chunkStart.Format("2006-01-02 15:04"), chunkEnd.Format("2006-01-02 15:04"))
			chunkStart = nextChunkStart(chunkStart, chunkEnd, time.Time{}, barDuration, false)
			if !chunkStart.Before(to) {
				break
			}
			continue
		}
		chunkNum++

		newBars := candlesAfter(lastTS, bars)
		if len(newBars) > 0 {
			if err := writeOrAppendCSV(path, newBars, fileExists); err != nil {
				return added, err
			}
			fileExists = true
			added += len(newBars)
			totalCount += len(newBars)
			lastTS = lastCandleTS(newBars)
		}

		lastBar := lastCandleTS(bars)
		logx.Info("[%s] чанк %d: +%d свечей, всего %d, до %s (пауза %s)",
			ticker, chunkNum, len(newBars), totalCount, lastBar.Format("2006-01-02 15:04"), throttle.CurrentDelay())

		chunkStart = nextChunkStart(chunkStart, chunkEnd, lastBar, barDuration, true)
		if !chunkStart.Before(to) {
			break
		}
	}
	return added, nil
}

// nextChunkStart — следующее окно: по последней свече или сдвиг через пустой участок.
func nextChunkStart(chunkStart, chunkEnd, lastBar time.Time, barDuration time.Duration, hadBars bool) time.Time {
	if hadBars {
		return lastBar.Add(barDuration)
	}
	if chunkEnd.After(chunkStart) {
		return chunkEnd
	}
	return chunkStart.Add(barDuration * maxBarsPerReq)
}

func fetchCandlesChunk(ctx context.Context, client *bcs.BCSClient, classCode, ticker, timeFrame string, from, to time.Time, cfg FetchConfig) ([]models.Candle, error) {
	cfg = cfg.Normalized()
	throttle := cfg.ThrottleOrDefault()
	var lastErr error

	for attempt := 0; attempt <= cfg.MaxRetries; attempt++ {
		if attempt > 0 {
			wait := cfg.RetryBase * time.Duration(1<<(attempt-1))
			if wait > cfg.RetryMax {
				wait = cfg.RetryMax
			}
			logx.Warn("[%s] rate limit / ошибка API, повтор %d/%d через %s", ticker, attempt, cfg.MaxRetries, wait)
			if err := sleepCtx(ctx, wait); err != nil {
				return nil, err
			}
		} else if cfg.Adaptive {
			if err := throttle.Wait(ctx); err != nil {
				return nil, err
			}
		}

		bars, retryAfter, err := fetchCandlesChunkOnce(ctx, client, classCode, ticker, timeFrame, from, to)
		if err == nil {
			sortCandlesByTime(bars)
			if cfg.Adaptive {
				throttle.OnSuccess()
			}
			return bars, nil
		}
		if !isRetryableAPIError(err) {
			return nil, err
		}
		if cfg.Adaptive {
			throttle.OnRateLimit(retryAfter)
		}
		lastErr = err
	}
	return nil, fmt.Errorf("исчерпаны повторы (%d): %w", cfg.MaxRetries, lastErr)
}

func fetchCandlesChunkOnce(ctx context.Context, client *bcs.BCSClient, classCode, ticker, timeFrame string, from, to time.Time) ([]models.Candle, time.Duration, error) {
	q := url.Values{}
	q.Set("ticker", ticker)
	q.Set("classCode", classCode)
	q.Set("timeFrame", timeFrame)
	q.Set("startDate", from.UTC().Format(time.RFC3339))
	q.Set("endDate", to.UTC().Format(time.RFC3339))

	reqURL := candlesChartURL + "?" + q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+client.AccessToken())

	resp, err := candlesHTTPClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("запрос свечей %s: %w", ticker, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, parseRetryAfter(resp.Header), fmt.Errorf("candles-chart %s: статус %d: %s", ticker, resp.StatusCode, string(body))
	}

	var parsed candlesResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, 0, fmt.Errorf("разбор ответа свечей: %w", err)
	}

	out := make([]models.Candle, 0, len(parsed.Bars))
	for _, bar := range parsed.Bars {
		ts, err := time.Parse(time.RFC3339, bar.Time)
		if err != nil {
			ts, err = time.Parse("2006-01-02T15:04:05", strings.TrimSuffix(bar.Time, "Z"))
			if err != nil {
				return nil, 0, fmt.Errorf("неверный timestamp %q: %w", bar.Time, err)
			}
		}
		out = append(out, models.Candle{
			Ticker:    ticker,
			Open:      bar.Open,
			High:      bar.High,
			Low:       bar.Low,
			Close:     bar.Close,
			Volume:    int64(bar.Volume),
			Timestamp: ts,
		})
	}
	return out, 0, nil
}

func parseRetryAfter(h http.Header) time.Duration {
	ra := strings.TrimSpace(h.Get("Retry-After"))
	if ra == "" {
		return 0
	}
	if sec, err := strconv.Atoi(ra); err == nil && sec > 0 {
		return time.Duration(sec) * time.Second
	}
	if t, err := http.ParseTime(ra); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	return 0
}

func sortCandlesByTime(candles []models.Candle) {
	sort.Slice(candles, func(i, j int) bool {
		return candles[i].Timestamp.Before(candles[j].Timestamp)
	})
}

func lastCandleTS(bars []models.Candle) time.Time {
	if len(bars) == 0 {
		return time.Time{}
	}
	max := bars[0].Timestamp
	for _, c := range bars[1:] {
		if c.Timestamp.After(max) {
			max = c.Timestamp
		}
	}
	return max
}

func candlesAfter(lastTS time.Time, bars []models.Candle) []models.Candle {
	if len(bars) == 0 {
		return nil
	}
	out := make([]models.Candle, 0, len(bars))
	for _, c := range bars {
		if lastTS.IsZero() || c.Timestamp.After(lastTS) {
			out = append(out, c)
		}
	}
	sortCandlesByTime(out)
	return dedupeCandles(out)
}

func isRetryableAPIError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "статус 429") ||
		strings.Contains(msg, "статус 503") ||
		strings.Contains(msg, "RATE_LIMIT")
}

func barDurationForTimeFrame(tf string) time.Duration {
	switch strings.ToUpper(tf) {
	case "M1":
		return time.Minute
	case "M5":
		return 5 * time.Minute
	case "M15":
		return 15 * time.Minute
	case "M30":
		return 30 * time.Minute
	case "H1":
		return time.Hour
	case "H4":
		return 4 * time.Hour
	case "D":
		return 24 * time.Hour
	default:
		return 5 * time.Minute
	}
}
