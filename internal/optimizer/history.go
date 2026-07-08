package optimizer

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"bcs-trading-bot/internal/bcs"
	"bcs-trading-bot/internal/optimizer/data"
	"bcs-trading-bot/pkg/logx"
	"bcs-trading-bot/pkg/models"
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
	return FetchCandlesWithConfig(ctx, client, classCode, ticker, timeFrame, from, to, data.DefaultFetchConfig())
}

// FetchCandlesWithConfig — FetchCandles с настраиваемым throttling/retry.
func FetchCandlesWithConfig(ctx context.Context, client *bcs.BCSClient, classCode, ticker, timeFrame string, from, to time.Time, cfg data.FetchConfig) ([]models.Candle, error) {
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
func fetchTickerRange(ctx context.Context, client *bcs.BCSClient, path, ticker, classCode, timeFrame string, existing []models.Candle, from, to time.Time, cfg data.FetchConfig) (int, error) {
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

func fetchCandlesChunk(ctx context.Context, client *bcs.BCSClient, classCode, ticker, timeFrame string, from, to time.Time, cfg data.FetchConfig) ([]models.Candle, error) {
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

func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
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

func dedupeCandles(candles []models.Candle) []models.Candle {
	if len(candles) == 0 {
		return candles
	}
	out := make([]models.Candle, 0, len(candles))
	var prev time.Time
	for _, c := range candles {
		if !c.Timestamp.Equal(prev) {
			out = append(out, c)
			prev = c.Timestamp
		}
	}
	return out
}

// MergeCandles объединяет два отсортированных набора свечей без дубликатов по timestamp.
func MergeCandles(existing, fresh []models.Candle) []models.Candle {
	if len(existing) == 0 {
		return dedupeCandles(fresh)
	}
	if len(fresh) == 0 {
		return existing
	}
	merged := append(append([]models.Candle(nil), existing...), fresh...)
	sort.Slice(merged, func(i, j int) bool {
		return merged[i].Timestamp.Before(merged[j].Timestamp)
	})
	return dedupeCandles(merged)
}

// TryLoadCSV читает CSV; если файла нет — возвращает nil без ошибки.
func TryLoadCSV(path, ticker string) ([]models.Candle, error) {
	candles, err := LoadCSV(path, ticker)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return candles, nil
}

// CandleDataRange возвращает min/max timestamp по всем загруженным тикерам.
func CandleDataRange(data map[string][]models.Candle) (from, to time.Time, ok bool) {
	for _, candles := range data {
		if len(candles) == 0 {
			continue
		}
		first := candles[0].Timestamp
		last := candles[len(candles)-1].Timestamp
		if !ok || first.Before(from) {
			from = first
		}
		if !ok || last.After(to) {
			to = last
		}
		ok = true
	}
	return from, to, ok
}

// AppendCSV дописывает свечи в существующий CSV (без заголовка).
func AppendCSV(path string, candles []models.Candle) error {
	if len(candles) == 0 {
		return nil
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()

	w := csv.NewWriter(f)
	for _, c := range candles {
		row := []string{
			c.Timestamp.Format(time.RFC3339),
			formatFloat(c.Open),
			formatFloat(c.High),
			formatFloat(c.Low),
			formatFloat(c.Close),
			strconv.FormatInt(c.Volume, 10),
		}
		if err := w.Write(row); err != nil {
			return err
		}
	}
	w.Flush()
	return w.Error()
}

func writeOrAppendCSV(path string, candles []models.Candle, fileExists bool) error {
	if len(candles) == 0 {
		return nil
	}
	if fileExists {
		return AppendCSV(path, candles)
	}
	return WriteCSV(path, candles)
}

// WriteCSV сохраняет свечи в CSV-файл.
func WriteCSV(path string, candles []models.Candle) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	w := csv.NewWriter(f)
	defer w.Flush()

	if err := w.Write([]string{"timestamp", "open", "high", "low", "close", "volume"}); err != nil {
		return err
	}
	for _, c := range candles {
		row := []string{
			c.Timestamp.Format(time.RFC3339),
			formatFloat(c.Open),
			formatFloat(c.High),
			formatFloat(c.Low),
			formatFloat(c.Close),
			strconv.FormatInt(c.Volume, 10),
		}
		if err := w.Write(row); err != nil {
			return err
		}
	}
	return w.Error()
}

// LoadCSV читает свечи из CSV-файла.
func LoadCSV(path, ticker string) ([]models.Candle, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	r := csv.NewReader(f)
	records, err := r.ReadAll()
	if err != nil {
		return nil, err
	}
	if len(records) < 2 {
		return nil, fmt.Errorf("%s: файл пуст или без данных", path)
	}

	header := records[0]
	col := mapColumns(header)
	out := make([]models.Candle, 0, len(records)-1)
	for _, row := range records[1:] {
		c, err := parseCandleRow(row, col, ticker)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Timestamp.Before(out[j].Timestamp)
	})
	return out, nil
}

func mapColumns(header []string) map[string]int {
	col := make(map[string]int, len(header))
	for i, h := range header {
		col[strings.ToLower(strings.TrimSpace(h))] = i
	}
	return col
}

func parseCandleRow(row []string, col map[string]int, ticker string) (models.Candle, error) {
	get := func(name string) (string, error) {
		i, ok := col[name]
		if !ok || i >= len(row) {
			return "", fmt.Errorf("колонка %q не найдена", name)
		}
		return strings.TrimSpace(row[i]), nil
	}

	tsStr, err := get("timestamp")
	if err != nil {
		return models.Candle{}, err
	}
	ts, err := time.Parse(time.RFC3339, tsStr)
	if err != nil {
		return models.Candle{}, fmt.Errorf("timestamp %q: %w", tsStr, err)
	}

	open, err := parseFloatCol(row, col, "open")
	if err != nil {
		return models.Candle{}, err
	}
	high, err := parseFloatCol(row, col, "high")
	if err != nil {
		return models.Candle{}, err
	}
	low, err := parseFloatCol(row, col, "low")
	if err != nil {
		return models.Candle{}, err
	}
	closePx, err := parseFloatCol(row, col, "close")
	if err != nil {
		return models.Candle{}, err
	}
	volStr, err := get("volume")
	if err != nil {
		return models.Candle{}, err
	}
	vol, err := strconv.ParseInt(volStr, 10, 64)
	if err != nil {
		return models.Candle{}, fmt.Errorf("volume: %w", err)
	}

	return models.Candle{
		Ticker:    ticker,
		Open:      open,
		High:      high,
		Low:       low,
		Close:     closePx,
		Volume:    vol,
		Timestamp: ts,
	}, nil
}

func parseFloatCol(row []string, col map[string]int, name string) (float64, error) {
	i, ok := col[name]
	if !ok || i >= len(row) {
		return 0, fmt.Errorf("колонка %q не найдена", name)
	}
	return strconv.ParseFloat(strings.TrimSpace(row[i]), 64)
}

func formatFloat(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

// FilterCandles возвращает свечи в полуинтервале [from, to).
func FilterCandles(candles []models.Candle, from, to time.Time) []models.Candle {
	out := make([]models.Candle, 0)
	for _, c := range candles {
		if !c.Timestamp.Before(from) && c.Timestamp.Before(to) {
			out = append(out, c)
		}
	}
	return out
}
