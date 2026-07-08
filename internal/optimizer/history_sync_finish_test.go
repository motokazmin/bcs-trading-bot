package optimizer

import (
	"os"
	"path/filepath"
	"testing"
)

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

type fmtError string

func (e fmtError) Error() string { return string(e) }
