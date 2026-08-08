package engine

import (
	"strings"
	"time"
)

const staleAgeBars = 3

// CandleBarDuration возвращает длительность бара по строке таймфрейма BCS (M1, M5, M15, H1).
// Неизвестный формат → M5.
func CandleBarDuration(tf string) time.Duration {
	return candleBarDuration(tf)
}

func candleBarDuration(tf string) time.Duration {
	switch strings.ToUpper(strings.TrimSpace(tf)) {
	case "M1":
		return time.Minute
	case "M5", "":
		return 5 * time.Minute
	case "M15":
		return 15 * time.Minute
	case "M30":
		return 30 * time.Minute
	case "H1":
		return time.Hour
	default:
		return 5 * time.Minute
	}
}

// candleMaxAge — максимальный допустимый |now − barTime| для live-входа (3×TF).
func candleMaxAge(tf string) time.Duration {
	return staleAgeBars * candleBarDuration(tf)
}

// candleFresh true, если метка бара достаточно близка к wall clock для live-entry.
func candleFresh(now, barTime time.Time, maxAge time.Duration) bool {
	if maxAge <= 0 {
		return true
	}
	age := now.Sub(barTime)
	if age < 0 {
		age = -age
	}
	return age <= maxAge
}
