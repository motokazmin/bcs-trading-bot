package data

import (
	"testing"
	"time"
)

func TestAdaptiveThrottleOnSuccess(t *testing.T) {
	th := NewAdaptiveThrottle(50*time.Millisecond, time.Second)
	th.OnSuccess()
	th.OnSuccess()
	if d := th.CurrentDelay(); d != 50*time.Millisecond {
		t.Fatalf("delay after successes: %v, want 50ms", d)
	}
}

func TestAdaptiveThrottleOnRateLimit(t *testing.T) {
	th := NewAdaptiveThrottle(50*time.Millisecond, time.Second)
	th.OnRateLimit(0)
	if d := th.CurrentDelay(); d != 100*time.Millisecond {
		t.Fatalf("delay after 429: %v, want 100ms", d)
	}
	th.OnRateLimit(500 * time.Millisecond)
	if d := th.CurrentDelay(); d != 500*time.Millisecond {
		t.Fatalf("delay after Retry-After: %v, want 500ms", d)
	}
	th.OnRateLimit(10 * time.Second)
	if d := th.CurrentDelay(); d != time.Second {
		t.Fatalf("delay capped at max: %v, want 1s", d)
	}
}
