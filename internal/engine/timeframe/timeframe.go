// Package timeframe — таймфреймы свечей: какие бывают у брокера, сколько длится бар,
// где лежит история каждого. Leaf: зависит только от models.
//
// История каждого таймфрейма — родные бары брокера в своей папке (HistoryDir), а не
// склейка из M5: так backtest видит те же бары, что live. Переключение таймфрейма —
// `sync-history -timeframe M15` один раз и строка `candle_timeframe` в YAML.
package timeframe

import (
	"fmt"
	"strings"
	"time"

	"bcs-trading-bot/internal/models"
)

// History — таймфрейм, история которого лежит в базовой папке (data/history).
// Остальные — рядом, в <база>-<tf> (HistoryDir).
const History = "M5"

// durations — таймфреймы, которые отдаёт БКС и которые бот умеет использовать.
// D — дневки (только для исследований: sync-history -timeframe D). Иное (например, M10) — ошибка.
var durations = map[string]time.Duration{
	"M1":  time.Minute,
	"M5":  5 * time.Minute,
	"M15": 15 * time.Minute,
	"M30": 30 * time.Minute,
	"H1":  time.Hour,
	"D":   24 * time.Hour,
}

// mskOffset — сдвиг МСК от UTC (без перехода на летнее время с 2014 года).
const mskOffset = 3 * time.Hour

// Normalize приводит запись таймфрейма к виду брокера ("m15 " → "M15").
func Normalize(tf string) string {
	return strings.ToUpper(strings.TrimSpace(tf))
}

// Duration — длительность бара. Неизвестный таймфрейм — ошибка, а не молчаливый M5:
// с неверной длительностью съезжают конец бара, EOD и гейты входа.
func Duration(tf string) (time.Duration, error) {
	d, ok := durations[Normalize(tf)]
	if !ok {
		return 0, fmt.Errorf("неизвестный таймфрейм %q (допустимо: M1, M5, M15, M30, H1, D)", tf)
	}
	return d, nil
}

// HistoryDir — папка истории таймфрейма tf: базовая для History, иначе <base>-<tf>
// (data/history → data/history-m15). M5 остаётся на месте: на его файлы ссылаются
// sha256 в docs/analysis/0008 и правила .gitignore.
func HistoryDir(base, tf string) string {
	tf = Normalize(tf)
	if tf == "" || tf == History {
		return base
	}
	return strings.TrimRight(base, "/") + "-" + strings.ToLower(tf)
}

// CheckGrid проверяет, что метки баров лежат на сетке таймфрейма tf. Ловит историю
// чужого таймфрейма (M5-файлы под видом M15): длительность бара тогда врёт, и
// с ней конец бара, EOD и гейты входа. Возвращает первую метку не на сетке.
func CheckGrid(candles []models.Candle, tf string) error {
	d, err := Duration(tf)
	if err != nil {
		return err
	}
	for _, c := range candles {
		// Сетка — по МСК: дневной бар БКС начинается в полночь МСК (21:00 UTC). Для внутридневных
		// таймфреймов сдвиг на целые часы сетку не меняет.
		at := c.Timestamp.Add(mskOffset)
		if !at.Truncate(d).Equal(at) {
			return fmt.Errorf("бар %s не на сетке %s — история другого таймфрейма?",
				c.Timestamp.UTC().Format(time.RFC3339), Normalize(tf))
		}
	}
	return nil
}
