package strategy_test

import (
	"encoding/json"
	"strings"
	"testing"

	"bcs-trading-bot/internal/strategy"
	"bcs-trading-bot/internal/trailing"
)

func TestEncodeParamsSnapshot(t *testing.T) {
	raw := strategy.EncodeParamsSnapshot("momentum_breakout", "atr", strategy.Params{
		"lookback":      20,
		"atrMultiplier": 1.0,
	}, trailing.DefaultConfig())
	if raw == "" {
		t.Fatal("empty snapshot")
	}
	if !strings.Contains(raw, `"strategy_type"`) || !strings.Contains(raw, `momentum_breakout`) {
		t.Fatalf("missing strategy type: %s", raw)
	}
	if !strings.Contains(raw, `"atrMultiplier":1`) {
		t.Fatalf("missing param: %s", raw)
	}
	var decoded strategy.ParamsSnapshot
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Params["lookback"] != 20 {
		t.Fatalf("lookback: %v", decoded.Params["lookback"])
	}
}
