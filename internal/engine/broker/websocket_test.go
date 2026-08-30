package broker

import (
	"testing"
	"time"
)

func moscowTime(hour, min int) time.Time {
	return time.Date(2026, 6, 25, hour, min, 0, 0, moscowLoc)
}

func TestIsWSQuietPeriod(t *testing.T) {
	tests := []struct {
		name  string
		when  time.Time
		quiet bool
	}{
		{"вечерка только что закрылась", moscowTime(23, 50), true},
		{"полночь", moscowTime(0, 0), true},
		{"глубокая ночь", moscowTime(3, 30), true},
		{"перед открытием", moscowTime(6, 59), true},
		{"утро открытие", moscowTime(7, 0), false},
		{"основная сессия", moscowTime(12, 0), false},
		{"вечерняя сессия", moscowTime(20, 0), false},
		{"до тихого окна", moscowTime(23, 49), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isWSQuietPeriod(tt.when); got != tt.quiet {
				t.Fatalf("isWSQuietPeriod(%s) = %v, want %v", tt.when.Format("15:04"), got, tt.quiet)
			}
		})
	}
}

func TestWSReadDeadline(t *testing.T) {
	if d := wsReadDeadline(moscowTime(2, 0)); d != wsReadDeadlineQuiet {
		t.Fatalf("ночью: got %s, want %s", d, wsReadDeadlineQuiet)
	}
	if d := wsReadDeadline(moscowTime(10, 0)); d != wsReadDeadlineActive {
		t.Fatalf("днём: got %s, want %s", d, wsReadDeadlineActive)
	}
}
