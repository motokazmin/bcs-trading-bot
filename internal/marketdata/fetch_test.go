package marketdata

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"bcs-trading-bot/pkg/models"
)

func TestIsRetryableAPIError(t *testing.T) {
	cases := []struct {
		err  error
		want bool
	}{
		{errors.New("candles-chart SBER: статус 429: RATE_LIMIT"), true},
		{errors.New("статус 503"), true},
		{errors.New("статус 401: unauthorized"), false},
		{nil, false},
	}
	for _, tc := range cases {
		if got := isRetryableAPIError(tc.err); got != tc.want {
			t.Fatalf("isRetryableAPIError(%v) = %v, want %v", tc.err, got, tc.want)
		}
	}
}

func TestFetchConfigNormalized(t *testing.T) {
	cfg := FetchConfig{}.Normalized()
	if cfg.ChunkDelay <= 0 || cfg.MaxRetries <= 0 {
		t.Fatalf("defaults not applied: %+v", cfg)
	}
	def := DefaultFetchConfig().Normalized()
	if !def.Adaptive || def.ChunkDelay != DefaultMinChunkDelay {
		t.Fatalf("default fetch config: %+v", def)
	}
}

func TestCandlesAfter(t *testing.T) {
	ts := time.Date(2024, 7, 1, 10, 0, 0, 0, time.UTC)
	bars := []models.Candle{
		{Timestamp: ts},
		{Timestamp: ts.Add(5 * time.Minute)},
		{Timestamp: ts.Add(10 * time.Minute)},
	}
	got := candlesAfter(ts, bars)
	if len(got) != 2 {
		t.Fatalf("got %d candles, want 2", len(got))
	}
	if !got[len(got)-1].Timestamp.Equal(ts.Add(10 * time.Minute)) {
		t.Fatalf("last ts: %v", got[len(got)-1].Timestamp)
	}
}

func TestLastCandleTSReverseAPIOrder(t *testing.T) {
	t0 := time.Date(2024, 7, 1, 10, 0, 0, 0, time.UTC)
	bars := []models.Candle{
		{Timestamp: t0.Add(10 * time.Minute)},
		{Timestamp: t0.Add(5 * time.Minute)},
		{Timestamp: t0},
	}
	want := t0.Add(10 * time.Minute)
	if got := lastCandleTS(bars); !got.Equal(want) {
		t.Fatalf("lastCandleTS: got %v, want %v", got, want)
	}
	sortCandlesByTime(bars)
	if !bars[len(bars)-1].Timestamp.Equal(want) {
		t.Fatalf("sorted last: %v", bars[len(bars)-1].Timestamp)
	}
}

func TestCandlesAfterReverseOrderNoDuplicates(t *testing.T) {
	t0 := time.Date(2024, 7, 1, 10, 0, 0, 0, time.UTC)
	lastTS := t0
	reverse := []models.Candle{
		{Timestamp: t0.Add(10 * time.Minute)},
		{Timestamp: t0.Add(5 * time.Minute)},
	}
	sortCandlesByTime(reverse)

	first := candlesAfter(lastTS, reverse)
	if len(first) != 2 {
		t.Fatalf("first batch: got %d, want 2", len(first))
	}
	lastTS = lastCandleTS(first)

	second := candlesAfter(lastTS, reverse)
	if len(second) != 0 {
		t.Fatalf("second batch: got %d duplicates, want 0", len(second))
	}

	nextStart := lastCandleTS(reverse).Add(5 * time.Minute)
	if !nextStart.Equal(t0.Add(15 * time.Minute)) {
		t.Fatalf("next chunk start: %v", nextStart)
	}
}

func TestNextChunkStartEmptySkip(t *testing.T) {
	bar := 5 * time.Minute
	start := time.Date(2025, 12, 30, 20, 50, 0, 0, time.UTC)
	end := start.Add(bar * maxBarsPerReq)

	got := nextChunkStart(start, end, time.Time{}, bar, false)
	if !got.Equal(end) {
		t.Fatalf("empty skip: got %v, want %v", got, end)
	}

	last := time.Date(2026, 1, 12, 10, 0, 0, 0, time.UTC)
	got = nextChunkStart(start, end, last, bar, true)
	if !got.Equal(last.Add(bar)) {
		t.Fatalf("after bars: got %v, want %v", got, last.Add(bar))
	}
}

func TestAppendCSV(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "SBER.csv")
	first := []models.Candle{{
		Timestamp: time.Date(2024, 7, 1, 10, 0, 0, 0, time.UTC),
		Open:      1, High: 2, Low: 0.5, Close: 1.5, Volume: 10,
	}}
	if err := WriteCSV(path, first); err != nil {
		t.Fatal(err)
	}
	second := []models.Candle{{
		Timestamp: time.Date(2024, 7, 1, 10, 5, 0, 0, time.UTC),
		Open:      2, High: 3, Low: 1.5, Close: 2.5, Volume: 20,
	}}
	if err := AppendCSV(path, second); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadCSV(path, "SBER")
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 2 {
		t.Fatalf("loaded %d candles, want 2", len(loaded))
	}
}
