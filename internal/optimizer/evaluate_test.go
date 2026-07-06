package optimizer

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"bcs-trading-bot/pkg/models"
)

func TestLoadCandleDataSkipsMissing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "SBER.csv")
	if err := WriteCSV(path, []models.Candle{{
		Timestamp: time.Date(2024, 7, 1, 10, 0, 0, 0, time.UTC),
		Open: 1, High: 2, Low: 0.5, Close: 1.5, Volume: 10,
	}}); err != nil {
		t.Fatal(err)
	}

	data, err := LoadCandleData(dir, []string{"SBER", "YNDX"})
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != 1 {
		t.Fatalf("loaded: got %d tickers, want 1", len(data))
	}
	if _, ok := data["SBER"]; !ok {
		t.Fatal("expected SBER")
	}
}

func TestLoadCandleDataAllMissing(t *testing.T) {
	dir := t.TempDir()
	_, err := LoadCandleData(dir, []string{"YNDX"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestFinishSyncHistoryPartialFailure(t *testing.T) {
	errs := []error{fmtError("YNDX: fail")}
	if err := finishSyncHistory(errs, 3); err != nil {
		t.Fatalf("partial failure should succeed: %v", err)
	}
}

func TestFinishSyncHistoryAllFailed(t *testing.T) {
	errs := []error{fmtError("A: fail"), fmtError("B: fail")}
	if err := finishSyncHistory(errs, 2); err == nil {
		t.Fatal("expected error when all tickers failed")
	}
}

type fmtError string

func (e fmtError) Error() string { return string(e) }

func TestWriteCSVCreatesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.csv")
	if err := WriteCSV(path, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}
