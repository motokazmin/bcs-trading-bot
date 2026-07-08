package data

import (
	"encoding/csv"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"bcs-trading-bot/pkg/models"
)

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
