package marketdata

import "time"

const (
	DefaultChunkDelay    = 400 * time.Millisecond
	DefaultMinChunkDelay = 50 * time.Millisecond
	DefaultMaxChunkDelay = 3 * time.Second
	DefaultTickerDelay   = 3 * time.Second
	DefaultMaxRetries    = 6
	DefaultRetryBase     = 5 * time.Second
	DefaultRetryMax      = 60 * time.Second
)

// FetchConfig — throttling и retry для candles-chart API.
type FetchConfig struct {
	ChunkDelay    time.Duration // мин. пауза (adaptive) или фиксированная
	MaxChunkDelay time.Duration // макс. пауза adaptive throttle
	TickerDelay   time.Duration // пауза между тикерами (последовательный режим)
	MaxRetries    int           // повторы при 429
	RetryBase     time.Duration // базовый backoff
	RetryMax      time.Duration // потолок backoff
	Adaptive      bool          // адаптивная пауза по ответам API
	Throttle      *AdaptiveThrottle
}

func DefaultFetchConfig() FetchConfig {
	return FetchConfig{
		ChunkDelay:    DefaultMinChunkDelay,
		MaxChunkDelay: DefaultMaxChunkDelay,
		TickerDelay:   DefaultTickerDelay,
		MaxRetries:    DefaultMaxRetries,
		RetryBase:     DefaultRetryBase,
		RetryMax:      DefaultRetryMax,
		Adaptive:      true,
	}
}

func (c FetchConfig) Normalized() FetchConfig {
	out := c
	if out.Adaptive {
		if out.ChunkDelay <= 0 {
			out.ChunkDelay = DefaultMinChunkDelay
		}
	} else if out.ChunkDelay <= 0 {
		out.ChunkDelay = DefaultChunkDelay
	}
	if out.MaxChunkDelay <= 0 {
		out.MaxChunkDelay = DefaultMaxChunkDelay
	}
	if out.TickerDelay <= 0 {
		out.TickerDelay = DefaultTickerDelay
	}
	if out.MaxRetries <= 0 {
		out.MaxRetries = DefaultMaxRetries
	}
	if out.RetryBase <= 0 {
		out.RetryBase = DefaultRetryBase
	}
	if out.RetryMax <= 0 {
		out.RetryMax = DefaultRetryMax
	}
	return out
}

func (c FetchConfig) ThrottleOrDefault() *AdaptiveThrottle {
	if c.Throttle != nil {
		return c.Throttle
	}
	return NewAdaptiveThrottle(c.ChunkDelay, c.MaxChunkDelay)
}
