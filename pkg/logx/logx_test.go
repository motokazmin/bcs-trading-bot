package logx

import (
	"bytes"
	"strings"
	"testing"
)

func TestPaintDisabled(t *testing.T) {
	SetColorEnabled(false)
	defer SetColorEnabled(detectColor())

	got := paint(red, "test")
	if got != "test" {
		t.Fatalf("paint disabled: got %q", got)
	}
}

func TestPnLFormatting(t *testing.T) {
	SetColorEnabled(false)

	if !strings.Contains(PnL(10.5), "10.50") {
		t.Fatalf("positive PnL: %q", PnL(10.5))
	}
	if !strings.HasPrefix(PnL(10.5), "+") {
		t.Fatalf("positive PnL prefix: %q", PnL(10.5))
	}
	if PnL(-3.2) != "-3.20" {
		t.Fatalf("negative PnL: %q", PnL(-3.2))
	}
}

func TestTradeCloseOutput(t *testing.T) {
	SetColorEnabled(false)

	var buf bytes.Buffer
	SetOutput(&buf)
	defer SetOutput(nil)

	TradeClose("SBER", "TAKE_PROFIT", 305.0, 150.25, 3.0)
	line := buf.String()
	if !strings.Contains(line, "[SBER]") {
		t.Fatalf("missing ticker: %q", line)
	}
	if !strings.Contains(line, "[TP]") {
		t.Fatalf("missing TP tag: %q", line)
	}
	if !strings.Contains(line, "+150.25") {
		t.Fatalf("missing PnL: %q", line)
	}
	if !strings.Contains(line, "+3.00R") {
		t.Fatalf("missing PnL R: %q", line)
	}
}
