package marketdata

import (
	"context"
	"sync"
	"time"
)

// AdaptiveThrottle подстраивает паузу между запросами: быстрее при успехе, медленнее при 429.
type AdaptiveThrottle struct {
	mu       sync.Mutex
	minDelay time.Duration
	maxDelay time.Duration
	current  time.Duration
}

func NewAdaptiveThrottle(minDelay, maxDelay time.Duration) *AdaptiveThrottle {
	if minDelay <= 0 {
		minDelay = 50 * time.Millisecond
	}
	if maxDelay <= 0 {
		maxDelay = 3 * time.Second
	}
	if maxDelay < minDelay {
		maxDelay = minDelay
	}
	return &AdaptiveThrottle{
		minDelay: minDelay,
		maxDelay: maxDelay,
		current:  minDelay,
	}
}

func (t *AdaptiveThrottle) Wait(ctx context.Context) error {
	t.mu.Lock()
	d := t.current
	t.mu.Unlock()
	return sleepCtx(ctx, d)
}

func (t *AdaptiveThrottle) OnSuccess() {
	t.mu.Lock()
	defer t.mu.Unlock()
	next := time.Duration(float64(t.current) * 0.85)
	if next < t.minDelay {
		next = t.minDelay
	}
	t.current = next
}

func (t *AdaptiveThrottle) OnRateLimit(retryAfter time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()
	next := t.current * 2
	if retryAfter > next {
		next = retryAfter
	}
	if next > t.maxDelay {
		next = t.maxDelay
	}
	if next < t.minDelay {
		next = t.minDelay
	}
	t.current = next
}

func (t *AdaptiveThrottle) CurrentDelay() time.Duration {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.current
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
