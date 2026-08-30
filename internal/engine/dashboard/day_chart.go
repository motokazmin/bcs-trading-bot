package dashboard

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"bcs-trading-bot/internal/models"
)

const dayTradesLimit = 5000

// DayTickerSummary — сводка по тикеру за день.
type DayTickerSummary struct {
	Ticker     string  `json:"ticker"`
	TradeCount int     `json:"trade_count"`
	TotalPnL   float64 `json:"total_pnl"`
}

func (s *Server) handleAPIDayTrades(w http.ResponseWriter, r *http.Request) {
	reader := s.requireReader(w)
	if reader == nil {
		return
	}
	date := strings.TrimSpace(r.URL.Query().Get("date"))
	if _, _, err := mskDayRange(date); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	f := parseFilter(r)
	f.DateFrom = date
	f.DateTo = date
	f.Ticker = "" // все тикеры дня; experiment/mode и др. фильтры сохраняем

	result, err := reader.ListClosedTrades(r.Context(), f, dayTradesLimit, 0)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	byTicker := map[string]*DayTickerSummary{}
	var totalPnL float64
	for _, t := range result.Trades {
		ticker := strings.ToUpper(strings.TrimSpace(t.Ticker))
		if ticker == "" {
			continue
		}
		sum, ok := byTicker[ticker]
		if !ok {
			sum = &DayTickerSummary{Ticker: ticker}
			byTicker[ticker] = sum
		}
		sum.TradeCount++
		sum.TotalPnL += t.GrossPnL
		totalPnL += t.GrossPnL
	}

	tickers := make([]DayTickerSummary, 0, len(byTicker))
	for _, sum := range byTicker {
		tickers = append(tickers, *sum)
	}
	sort.Slice(tickers, func(i, j int) bool {
		if tickers[i].TradeCount != tickers[j].TradeCount {
			return tickers[i].TradeCount > tickers[j].TradeCount
		}
		return tickers[i].Ticker < tickers[j].Ticker
	})

	writeJSON(w, map[string]any{
		"date":        date,
		"tickers":     tickers,
		"trade_count": result.Total,
		"total_pnl":   totalPnL,
	})
}

func (s *Server) handleAPIDayChart(w http.ResponseWriter, r *http.Request) {
	reader := s.requireReader(w)
	if reader == nil {
		return
	}
	if s.candles == nil {
		http.Error(w, "провайдер свечей не настроен", http.StatusServiceUnavailable)
		return
	}

	date := strings.TrimSpace(r.URL.Query().Get("date"))
	ticker := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("ticker")))
	if ticker == "" {
		http.Error(w, "ticker обязателен", http.StatusBadRequest)
		return
	}
	if _, _, err := mskDayRange(date); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	f := parseFilter(r)
	f.DateFrom = date
	f.DateTo = date
	f.Ticker = ticker

	result, err := reader.ListClosedTrades(r.Context(), f, dayTradesLimit, 0)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	trades := sortTradesByOpen(result.Trades)
	tf := resolveDayTimeframe(trades, r.URL.Query().Get("timeframe"))

	candles, err := s.candles.DayCandles(r.Context(), ticker, tf, date)
	if err != nil {
		http.Error(w, fmt.Sprintf("свечи: %v", err), http.StatusBadGateway)
		return
	}

	writeJSON(w, BuildDayChartPayload(date, ticker, tf, candles, trades))
}

func resolveDayTimeframe(trades []models.ClosedTrade, override string) string {
	if tf := strings.ToUpper(strings.TrimSpace(override)); tf != "" {
		return tf
	}
	for _, t := range trades {
		if tf := strings.ToUpper(strings.TrimSpace(t.CandleTimeframe)); tf != "" {
			return tf
		}
	}
	return "M5"
}

func sortTradesByOpen(trades []models.ClosedTrade) []models.ClosedTrade {
	sorted := append([]models.ClosedTrade(nil), trades...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].OpenedAt.Equal(sorted[j].OpenedAt) {
			return sorted[i].ClosedAt.Before(sorted[j].ClosedAt)
		}
		return sorted[i].OpenedAt.Before(sorted[j].OpenedAt)
	})
	return sorted
}

// BuildDayChartPayload — candles/markers/trades для графика дня.
func BuildDayChartPayload(date, ticker, timeframe string, candles []models.Candle, trades []models.ClosedTrade) map[string]any {
	outCandles := candlesToPayload(candles)

	sorted := sortTradesByOpen(trades)
	markers := closedTradesToMarkers(sorted)
	spans := closedTradesToSpans(sorted)

	return map[string]any{
		"date":      date,
		"ticker":    ticker,
		"timeframe": timeframe,
		"candles":   candlesOrEmpty(outCandles),
		"markers":   markers,
		"trades":    spans,
	}
}

func closedTradesToMarkers(trades []models.ClosedTrade) []map[string]any {
	markers := make([]map[string]any, 0, len(trades)*2)
	for i, t := range trades {
		n := i + 1
		isLong := strings.EqualFold(t.Direction, "BUY")
		entryPos := "belowBar"
		entryColor := "#26a69a"
		entryShape := "arrowUp"
		exitPos := "aboveBar"
		if !isLong {
			entryPos = "aboveBar"
			entryColor = "#ef5350"
			entryShape = "arrowDown"
			exitPos = "belowBar"
		}
		markers = append(markers, map[string]any{
			"time":     t.OpenedAt.Unix(),
			"position": entryPos,
			"color":    entryColor,
			"shape":    entryShape,
			"text":     fmt.Sprintf("%d ВХ %s", n, formatMarkerPrice(t.EntryPrice)),
			"size":     2,
		})
		exitColor := "#e57373"
		if t.GrossPnL > 0 {
			exitColor = "#66bb6a"
		}
		markers = append(markers, map[string]any{
			"time":     t.ClosedAt.Unix(),
			"position": exitPos,
			"color":    exitColor,
			"shape":    "circle",
			"text":     fmt.Sprintf("%d ВЫХ %s %s", n, closeReasonShort(t.CloseReason), formatMarkerPrice(t.ExitPrice)),
			"size":     2,
		})
	}
	sort.Slice(markers, func(i, j int) bool {
		ti, _ := markers[i]["time"].(int64)
		tj, _ := markers[j]["time"].(int64)
		return ti < tj
	})
	return markers
}

func formatMarkerPrice(price float64) string {
	return strconv.FormatFloat(price, 'f', 2, 64)
}

func closedTradesToSpans(trades []models.ClosedTrade) []map[string]any {
	out := make([]map[string]any, len(trades))
	for i, t := range trades {
		out[i] = map[string]any{
			"index":            i + 1,
			"direction":        strings.ToUpper(t.Direction),
			"entryTime":        t.OpenedAt.Unix(),
			"exitTime":         t.ClosedAt.Unix(),
			"entryPrice":       t.EntryPrice,
			"exitPrice":        t.ExitPrice,
			"pnl":              t.GrossPnL,
			"pnlR":             t.PnLR,
			"quantity":         t.Quantity,
			"experimentId":     t.ExperimentID,
			"entryLabel":       formatOpenTimeMSK(t.OpenedAt),
			"exitLabel":        formatOpenTimeMSK(t.ClosedAt),
			"closeReason":      t.CloseReason,
			"closeReasonLabel": closeReasonLabel(t.CloseReason),
		}
	}
	return out
}

func closeReasonShort(reason string) string {
	switch reason {
	case models.CloseReasonEOD:
		return "EOD"
	case models.CloseReasonStopLoss:
		return "SL"
	case models.CloseReasonTakeProfit:
		return "TP"
	case models.CloseReasonSmoke:
		return "SMOKE"
	default:
		if reason == "" {
			return "?"
		}
		return reason
	}
}

func closeReasonLabel(reason string) string {
	switch reason {
	case models.CloseReasonEOD:
		return "EOD — конец дня"
	case models.CloseReasonStopLoss:
		return "Стоп-лосс"
	case models.CloseReasonTakeProfit:
		return "Тейк-профит"
	case models.CloseReasonSmoke:
		return "Smoke-тест"
	default:
		if reason == "" {
			return "—"
		}
		return reason
	}
}
