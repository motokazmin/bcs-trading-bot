package engine

import (
	"time"

	"bcs-trading-bot/internal/engine/timeframe"
)

const staleAgeBars = 3

// CandleBarDuration возвращает длительность бара по строке таймфрейма BCS (M1, M5, M15, M30, H1).
// Пустой или неизвестный формат → M5; конфиг с неизвестным таймфреймом не загрузится
// (config.validate), так что fallback здесь — только для пустого поля в тестах.
func CandleBarDuration(tf string) time.Duration {
	return candleBarDuration(tf)
}

func candleBarDuration(tf string) time.Duration {
	if d, err := timeframe.Duration(tf); err == nil {
		return d
	}
	return 5 * time.Minute
}

// candleMaxAge — максимальный допустимый |now − barTime| для live-входа (3×TF).
func candleMaxAge(tf string) time.Duration {
	return staleAgeBars * candleBarDuration(tf)
}

// CandleFreshForEntry — можно ли открывать позицию по бару с меткой barTime
// на таймфрейме tf при текущем wall clock now. Порог — 3×TF (см. staleAgeBars):
// защита от входа по переигранному/бэкфилл-бару после реконнекта WS.
// Используется internal/strategy/selfmanaged при обработке закрытой свечи.
func CandleFreshForEntry(now, barTime time.Time, tf string) bool {
	return candleFresh(now, barTime, candleMaxAge(tf))
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
