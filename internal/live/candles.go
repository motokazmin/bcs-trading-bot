package live

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"bcs-trading-bot/internal/bcs"
	"bcs-trading-bot/internal/marketdata"
	"bcs-trading-bot/pkg/models"
)

const defaultCandleCacheTTL = 15 * time.Minute

// DayCandleProvider загружает свечи за торговый день (MSK).
type DayCandleProvider interface {
	DayCandles(ctx context.Context, ticker, timeFrame, dateYYYYMMDD string) ([]models.Candle, error)
}

// CandleFetcher — низкоуровневая загрузка свечей (BCS или мок в тестах).
type CandleFetcher interface {
	FetchCandles(ctx context.Context, classCode, ticker, timeFrame string, from, to time.Time) ([]models.Candle, error)
}

// BCSCandleFetcher загружает свечи через BCS candles-chart API.
type BCSCandleFetcher struct {
	Client *bcs.BCSClient
}

func (f *BCSCandleFetcher) FetchCandles(ctx context.Context, classCode, ticker, timeFrame string, from, to time.Time) ([]models.Candle, error) {
	if f == nil || f.Client == nil {
		return nil, fmt.Errorf("BCS клиент не задан")
	}
	return marketdata.FetchCandles(ctx, f.Client, classCode, ticker, timeFrame, from, to)
}

type candleCacheEntry struct {
	candles []models.Candle
	expires time.Time
}

// CachedDayCandles — DayCandleProvider с in-memory TTL-кэшем.
type CachedDayCandles struct {
	Fetcher   CandleFetcher
	ClassCode string
	TTL       time.Duration

	mu    sync.Mutex
	cache map[string]candleCacheEntry
}

// NewCachedDayCandles создаёт провайдер с кэшем (по умолчанию 15 мин).
func NewCachedDayCandles(fetcher CandleFetcher, classCode string, ttl time.Duration) *CachedDayCandles {
	if ttl <= 0 {
		ttl = defaultCandleCacheTTL
	}
	return &CachedDayCandles{
		Fetcher:   fetcher,
		ClassCode: strings.TrimSpace(classCode),
		TTL:       ttl,
		cache:     make(map[string]candleCacheEntry),
	}
}

func (p *CachedDayCandles) DayCandles(ctx context.Context, ticker, timeFrame, dateYYYYMMDD string) ([]models.Candle, error) {
	if p == nil || p.Fetcher == nil {
		return nil, fmt.Errorf("провайдер свечей не настроен")
	}
	ticker = strings.ToUpper(strings.TrimSpace(ticker))
	timeFrame = strings.ToUpper(strings.TrimSpace(timeFrame))
	if ticker == "" {
		return nil, fmt.Errorf("ticker обязателен")
	}
	if timeFrame == "" {
		timeFrame = "M5"
	}
	from, to, err := mskDayRange(dateYYYYMMDD)
	if err != nil {
		return nil, err
	}

	key := ticker + "|" + dateYYYYMMDD + "|" + timeFrame
	now := time.Now()
	p.mu.Lock()
	if ent, ok := p.cache[key]; ok && now.Before(ent.expires) {
		out := append([]models.Candle(nil), ent.candles...)
		p.mu.Unlock()
		return out, nil
	}
	p.mu.Unlock()

	candles, err := p.Fetcher.FetchCandles(ctx, p.ClassCode, ticker, timeFrame, from, to)
	if err != nil {
		return nil, err
	}
	if candles == nil {
		candles = []models.Candle{}
	}

	p.mu.Lock()
	p.cache[key] = candleCacheEntry{
		candles: append([]models.Candle(nil), candles...),
		expires: now.Add(p.TTL),
	}
	p.mu.Unlock()
	return candles, nil
}

func mskDayRange(dateYYYYMMDD string) (from, to time.Time, err error) {
	dateYYYYMMDD = strings.TrimSpace(dateYYYYMMDD)
	if dateYYYYMMDD == "" {
		return time.Time{}, time.Time{}, fmt.Errorf("date обязателен (YYYY-MM-DD)")
	}
	loc := time.FixedZone("MSK", 3*3600)
	day, err := time.ParseInLocation("2006-01-02", dateYYYYMMDD, loc)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("date: ожидается YYYY-MM-DD: %w", err)
	}
	return day, day.Add(24 * time.Hour), nil
}
