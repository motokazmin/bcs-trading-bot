package charts

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	core "bcs-trading-bot/internal/optimizer/core"
	evalpkg "bcs-trading-bot/internal/optimizer/eval"
	"bcs-trading-bot/pkg/models"
)

func TestEvaluatePeriodDetailedMatchesMetrics(t *testing.T) {
	candles := []models.Candle{
		{Ticker: "T", Close: 100, Timestamp: time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC)},
		{Ticker: "T", Close: 101, Timestamp: time.Date(2024, 1, 1, 10, 5, 0, 0, time.UTC)},
	}
	data := map[string][]models.Candle{"T": candles}
	settings := evalpkg.RunSettings{Tickers: []string{"T"}, StrategyID: "momentum_breakout", StopMode: "atr"}
	e := evalpkg.NewEvaluator(settings, &core.SearchSpace{
		Strategy:   "momentum_breakout",
		Parameters: map[string]core.ParamBounds{"lookback": {Type: core.ParamInt, Min: 1, Max: 2}},
	}, data)

	from := candles[0].Timestamp
	to := candles[len(candles)-1].Timestamp.Add(time.Minute)
	params := core.ParameterSet{"lookback": 1}

	metrics := e.EvaluatePeriod(context.Background(), params, from, to)
	detailed := e.EvaluatePeriodDetailed(context.Background(), params, from, to)

	if detailed.Metrics != metrics {
		t.Fatalf("metrics differ: detailed=%+v metrics=%+v", detailed.Metrics, metrics)
	}
}

func TestBuildChartHTMLContainsData(t *testing.T) {
	now := time.Date(2024, 6, 1, 10, 0, 0, 0, time.UTC)
	candles := []models.Candle{
		{Ticker: "SBER", Close: 100, Timestamp: now},
		{Ticker: "SBER", Close: 101, Timestamp: now.Add(5 * time.Minute)},
	}
	trades := []models.ClosedTrade{{
		Ticker:     "SBER",
		Direction:  "BUY",
		Quantity:   1,
		EntryPrice: 100,
		ExitPrice:  101,
		GrossPnL:   10,
		OpenedAt:   now,
		ClosedAt:   now.Add(5 * time.Minute),
	}}

	html, err := BuildChartHTML(ChartMeta{
		Experiment: "momentum_breakout",
		Ticker:     "SBER",
		Strategy:   "momentum_breakout",
		PeriodFrom: now,
		PeriodTo:   now.Add(time.Hour),
		Metrics:    core.Metrics{NumTrades: 1, TotalPnL: 5},
	}, candles, trades, 5)
	if err != nil {
		t.Fatal(err)
	}
	body := string(html)
	if !strings.Contains(body, "momentum_breakout / SBER") {
		t.Fatal("missing title")
	}
	if strings.Contains(body, "__CHART_DATA__") {
		t.Fatal("chart data placeholder not replaced")
	}
	if !strings.Contains(body, `"shape":"arrowUp"`) {
		t.Fatal("missing entry marker")
	}
	if !strings.Contains(body, `"shape":"square"`) {
		t.Fatal("missing exit marker")
	}
	if !strings.Contains(body, `"entryTime"`) || !strings.Contains(body, `"exitTime"`) {
		t.Fatal("missing trade span data")
	}
	if !strings.Contains(body, `id="trades-list"`) || !strings.Contains(body, `id="reset-zoom"`) {
		t.Fatal("missing trades panel")
	}
	if !strings.Contains(body, `"closeReasonLabel"`) {
		t.Fatal("missing close reason in trade span")
	}
	if !strings.Contains(body, "время МСК") {
		t.Fatal("missing MSK timezone hint")
	}
	if !strings.Contains(body, "tickMarkFormatter") || !strings.Contains(body, "formatMSKTick") {
		t.Fatal("missing MSK tick mark formatter for time axis")
	}
	if !strings.Contains(body, "tickLabelContext") || !strings.Contains(body, "subscribeVisibleTimeRangeChange") {
		t.Fatal("missing adaptive time axis labels")
	}
	if !strings.Contains(body, "effectiveVisibleSpan") || !strings.Contains(body, "formatMSKCrosshairInfo") {
		t.Fatal("missing robust year label logic")
	}
	if !strings.Contains(body, `id="crosshair-info"`) || !strings.Contains(body, "subscribeCrosshairMove") {
		t.Fatal("missing crosshair price/time info panel")
	}
	if !strings.Contains(body, "fullDataLogicalRange") || !strings.Contains(body, "saveBaselineView") {
		t.Fatal("missing full-range baseline zoom logic")
	}
	if !strings.Contains(body, "minBarSpacing") || !strings.Contains(body, "FULL_VIEW_MIN_BAR_SPACING") {
		t.Fatal("missing dense-data min bar spacing for full view")
	}
	if !strings.Contains(body, "makeVisibleWindowAutoscale") || !strings.Contains(body, "refreshTradePriceScale") {
		t.Fatal("missing dynamic Y-axis autoscale on pan/zoom")
	}
	if !strings.Contains(body, "updateResetButtonState") || !strings.Contains(body, "isAtBaselineView") {
		t.Fatal("missing reset button state for manual chart zoom")
	}
	if !strings.Contains(body, "Win rate") || !strings.Contains(body, "5 ₽") {
		t.Fatalf("missing stats panel: %s", body)
	}
}

func TestTradesToChartSpans(t *testing.T) {
	now := time.Date(2024, 1, 1, 7, 0, 0, 0, time.UTC)
	spans := tradesToChartSpans([]models.ClosedTrade{{
		Direction:   "buy",
		GrossPnL:    100,
		Quantity:    2,
		EntryPrice:  100,
		ExitPrice:   110,
		OpenedAt:    now,
		ClosedAt:    now.Add(time.Minute),
		CloseReason: models.CloseReasonEOD,
	}}, 5)
	if len(spans) != 1 {
		t.Fatalf("spans: got %d", len(spans))
	}
	if spans[0].Direction != "BUY" {
		t.Fatalf("direction: %s", spans[0].Direction)
	}
	if spans[0].EntryLabel != "2024-01-01 10:00 МСК" {
		t.Fatalf("entry label: %s", spans[0].EntryLabel)
	}
	if spans[0].CloseReasonLabel != "EOD — конец дня" {
		t.Fatalf("close reason: %s", spans[0].CloseReasonLabel)
	}
	if spans[0].PnL != 90 {
		t.Fatalf("pnl: %v", spans[0].PnL)
	}
}

func TestFormatCloseReasonShort(t *testing.T) {
	if got := formatCloseReasonShort(models.CloseReasonStopLoss); got != "SL" {
		t.Fatalf("got %s", got)
	}
	if got := formatCloseReasonShort(models.CloseReasonEOD); got != "EOD" {
		t.Fatalf("got %s", got)
	}
}

func TestTradesToChartMarkersPnL(t *testing.T) {
	now := time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC)
	markers := tradesToChartMarkers([]models.ClosedTrade{{
		Direction:   "SELL",
		GrossPnL:    100,
		Quantity:    2,
		OpenedAt:    now,
		ClosedAt:    now.Add(time.Minute),
		CloseReason: models.CloseReasonTakeProfit,
	}}, 5)
	if len(markers) != 2 {
		t.Fatalf("markers: got %d", len(markers))
	}
	if markers[0].Shape != "arrowDown" {
		t.Fatalf("entry shape: %s", markers[0].Shape)
	}
	if markers[1].Text != "1 ВЫХ TP" {
		t.Fatalf("exit text: %s", markers[1].Text)
	}
}

func TestListExperimentDirs(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"exp-mean_reversion", "exp-opening_range", "exp-empty", "other"} {
		path := filepath.Join(dir, name)
		if err := os.Mkdir(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "exp-mean_reversion", "best-config-20260101.yaml"), []byte("tickers: [SBER]"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "exp-opening_range", "best-config-20260101.yaml"), []byte("tickers: [SBER]"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := ListExperimentDirs(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "mean_reversion" || got[1] != "opening_range" {
		t.Fatalf("got %#v", got)
	}
}

func TestResolveChartsExperimentDir(t *testing.T) {
	dir := t.TempDir()
	exp := filepath.Join(dir, "exp-mean_reversion")
	if err := os.Mkdir(exp, 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := resolveChartsExperimentDir(ChartsOptions{ExperimentDir: exp})
	if err != nil {
		t.Fatal(err)
	}
	if got != exp {
		t.Fatalf("got %s", got)
	}

	got2, err := resolveChartsExperimentDir(ChartsOptions{
		ResultsDir: dir,
		Experiment: "mean_reversion",
	})
	if err != nil || got2 != exp {
		t.Fatalf("via results dir: %s err=%v", got2, err)
	}
}

func TestResolveExperimentDir(t *testing.T) {
	dir := t.TempDir()
	exp := filepath.Join(dir, "exp-mean_reversion")
	if err := os.Mkdir(exp, 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := ResolveExperimentDir(dir, "mean_reversion")
	if err != nil {
		t.Fatal(err)
	}
	if got != exp {
		t.Fatalf("got %s", got)
	}

	got2, err := ResolveExperimentDir(dir, "exp-mean_reversion")
	if err != nil || got2 != exp {
		t.Fatalf("exp- prefix: %s err=%v", got2, err)
	}
}

func TestLoadLatestBestConfigPath(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "best-config-20260101.yaml"), []byte("tickers: [SBER]"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "best-config-20260201.yaml"), []byte("tickers: [SBER]"), 0o644); err != nil {
		t.Fatal(err)
	}
	path, err := LoadLatestBestConfigPath(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(path, "best-config-20260201.yaml") {
		t.Fatalf("got %s", path)
	}
}

func TestChartStats(t *testing.T) {
	stats := chartStats(core.Metrics{
		NumTrades:   4,
		TotalPnL:    -120,
		WinRate:     0.25,
		MaxDrawdown: 200,
	}, []models.ClosedTrade{
		{GrossPnL: 50, Quantity: 1, RDistance: 1, StepPriceValue: 1, PnLR: 0.5},
		{GrossPnL: -30, Quantity: 1, RDistance: 1, StepPriceValue: 1, PnLR: -0.3},
		{GrossPnL: -40, Quantity: 1, RDistance: 1, StepPriceValue: 1, PnLR: -0.4},
		{GrossPnL: -100, Quantity: 1, RDistance: 1, StepPriceValue: 1, PnLR: -1.0},
	}, 0)
	if len(stats) < 6 {
		t.Fatalf("stats: %d", len(stats))
	}
	if stats[0].Value != "4" {
		t.Fatalf("trades stat: %+v", stats[0])
	}
	if stats[1].Label != "Expectancy (R)" {
		t.Fatalf("expected expectancy first, got: %+v", stats[1])
	}
	if stats[2].Value != "-120 ₽" {
		t.Fatalf("unexpected pnl stat: %+v", stats[2])
	}
}

func TestTradeChartWindow(t *testing.T) {
	t0 := time.Date(2024, 6, 10, 10, 0, 0, 0, time.UTC)
	t1 := time.Date(2024, 8, 15, 15, 0, 0, 0, time.UTC)
	padding := 24 * time.Hour
	from, to, ok := tradeChartWindow([]models.ClosedTrade{
		{OpenedAt: t0, ClosedAt: t0.Add(time.Hour)},
		{OpenedAt: t1, ClosedAt: t1.Add(2 * time.Hour)},
	}, padding)
	if !ok {
		t.Fatal("expected window")
	}
	if !from.Equal(t0.Add(-padding)) {
		t.Fatalf("from: got %v want %v", from, t0.Add(-padding))
	}
	if !to.Equal(t1.Add(2*time.Hour).Add(padding)) {
		t.Fatalf("to: got %v", to)
	}
}

func TestBuildChartHTMLZoomsToTrades(t *testing.T) {
	base := time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC)
	var candles []models.Candle
	for i := 0; i < 100; i++ {
		candles = append(candles, models.Candle{
			Close:     100,
			Timestamp: base.Add(time.Duration(i) * time.Minute),
		})
	}
	far := base.Add(365 * 24 * time.Hour)
	for i := 0; i < 100; i++ {
		candles = append(candles, models.Candle{
			Close:     200,
			Timestamp: far.Add(time.Duration(i) * time.Minute),
		})
	}
	tradeAt := base.Add(50 * time.Minute)
	trades := []models.ClosedTrade{{
		Direction: "BUY",
		GrossPnL:  10,
		Quantity:  1,
		OpenedAt:  tradeAt,
		ClosedAt:  tradeAt.Add(30 * time.Minute),
	}}

	html, err := BuildChartHTML(ChartMeta{
		Experiment: "test",
		Ticker:     "SBER",
		Strategy:   "test",
		PeriodFrom: base,
		PeriodTo:   far,
		Metrics:    core.Metrics{NumTrades: 1, TotalPnL: 5, WinRate: 1},
	}, candles, trades, 0)
	if err != nil {
		t.Fatal(err)
	}
	count := strings.Count(string(html), `"close":`)
	if count >= 150 {
		t.Fatalf("expected zoomed chart, got %d candle points", count)
	}
}

func TestChartPayloadJSON(t *testing.T) {
	now := time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC)
	payload := chartPayload{
		Candles: candlesToChartCandles([]models.Candle{{Open: 99, High: 101, Low: 98, Close: 100, Timestamp: now}}),
		Markers: tradesToChartMarkers(nil, 0),
		Trades:  tradesToChartSpans(nil, 0),
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(data) {
		t.Fatal("invalid json")
	}
	if math.IsNaN(payload.Candles[0].Close) {
		t.Fatal("nan value")
	}
}
