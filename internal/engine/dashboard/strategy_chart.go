package dashboard

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"bcs-trading-bot/internal/models"
)

const (
	strategyTradesLimit      = 5000
	tradeChartPaddingBefore  = 3 * time.Hour
	tradeChartPaddingAfter   = 1 * time.Hour
)

func (s *Server) handleAPIStrategyTrades(w http.ResponseWriter, r *http.Request) {
	reader := s.requireReader(w)
	if reader == nil {
		return
	}
	experimentID := strings.TrimSpace(r.URL.Query().Get("experiment_id"))
	if experimentID == "" {
		http.Error(w, "experiment_id обязателен", http.StatusBadRequest)
		return
	}

	f := s.parseFilter(r)
	f.ExperimentID = experimentID

	result, err := reader.ListClosedTrades(r.Context(), f, strategyTradesLimit, 0)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	trades := sortTradesByOpen(result.Trades)
	spans := closedTradesToSpans(trades)
	for i := range spans {
		id := tradeSpanID(trades[i])
		spans[i]["tradeId"] = id
		spans[i]["trade_id"] = id
		spans[i]["ticker"] = strings.ToUpper(trades[i].Ticker)
		spans[i]["tradingDate"] = trades[i].TradingDate
		spans[i]["trading_date"] = trades[i].TradingDate
	}

	summary, err := reader.GetSummary(r.Context(), f)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, map[string]any{
		"experiment_id": experimentID,
		"trade_count":   result.Total,
		"total_pnl":     summary.TotalPnL,
		"win_rate":      summary.WinRate,
		"expectancy_r":  summary.ExpectancyR,
		"trades":        spans,
	})
}

func (s *Server) handleAPIStrategyTradeChart(w http.ResponseWriter, r *http.Request) {
	reader := s.requireReader(w)
	if reader == nil {
		return
	}
	if s.candles == nil {
		http.Error(w, "провайдер свечей не настроен", http.StatusServiceUnavailable)
		return
	}

	experimentID := strings.TrimSpace(r.URL.Query().Get("experiment_id"))
	ticker := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("ticker")))
	tradeID := strings.TrimSpace(r.URL.Query().Get("trade_id"))
	if experimentID == "" {
		http.Error(w, "experiment_id обязателен", http.StatusBadRequest)
		return
	}
	if ticker == "" {
		http.Error(w, "ticker обязателен", http.StatusBadRequest)
		return
	}
	if tradeID == "" {
		http.Error(w, "trade_id обязателен", http.StatusBadRequest)
		return
	}

	f := s.parseFilter(r)
	f.ExperimentID = experimentID
	f.Ticker = ticker

	result, err := reader.ListClosedTrades(r.Context(), f, strategyTradesLimit, 0)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var trade *models.ClosedTrade
	for _, t := range result.Trades {
		if tradeSpanID(t) == tradeID {
			cp := t
			trade = &cp
			break
		}
	}
	if trade == nil {
		http.Error(w, "сделка не найдена", http.StatusNotFound)
		return
	}

	tf := resolveDayTimeframe([]models.ClosedTrade{*trade}, r.URL.Query().Get("timeframe"))
	from, to := singleTradeChartRange(*trade)
	candles, err := s.candles.RangeCandles(r.Context(), ticker, tf, from, to)
	if err != nil {
		if trade.TradingDate != "" {
			candles, err = s.candles.DayCandles(r.Context(), ticker, tf, trade.TradingDate)
		}
		if err != nil {
			http.Error(w, fmt.Sprintf("свечи: %v", err), http.StatusBadGateway)
			return
		}
	}
	if len(candles) == 0 && trade.TradingDate != "" {
		if dayCandles, dayErr := s.candles.DayCandles(r.Context(), ticker, tf, trade.TradingDate); dayErr == nil {
			candles = dayCandles
		}
	}

	writeJSON(w, BuildStrategyTradeChartPayload(experimentID, tf, candles, *trade))
}

func tradeSpanID(t models.ClosedTrade) string {
	return fmt.Sprintf("%s-%d", strings.ToUpper(t.Ticker), t.OpenedAt.Unix())
}

func singleTradeChartRange(t models.ClosedTrade) (from, to time.Time) {
	from = t.OpenedAt.Add(-tradeChartPaddingBefore)
	to = t.ClosedAt.Add(tradeChartPaddingAfter)
	if to.Before(from) {
		to = from.Add(time.Hour)
	}
	return from, to
}

// BuildStrategyTradeChartPayload — candles/markers/levels для одной сделки.
func BuildStrategyTradeChartPayload(experimentID, timeframe string, candles []models.Candle, trade models.ClosedTrade) map[string]any {
	outCandles := candlesToPayload(candles)
	markers := closedTradesToMarkers([]models.ClosedTrade{trade})
	spans := closedTradesToSpans([]models.ClosedTrade{trade})
	if len(spans) > 0 {
		spans[0]["tradeId"] = tradeSpanID(trade)
		spans[0]["ticker"] = strings.ToUpper(trade.Ticker)
	}

	return map[string]any{
		"experiment_id": experimentID,
		"ticker":        strings.ToUpper(trade.Ticker),
		"timeframe":     timeframe,
		"trade_id":      tradeSpanID(trade),
		"trading_date":  trade.TradingDate,
		"candles":       candlesOrEmpty(outCandles),
		"markers":       markers,
		"trades":        spans,
		"levels":        closedTradeLevels(trade),
		"trade":         spans[0],
	}
}

func candlesToPayload(candles []models.Candle) []map[string]any {
	out := make([]map[string]any, len(candles))
	for i, c := range candles {
		out[i] = map[string]any{
			"time":  c.Timestamp.Unix(),
			"open":  c.Open,
			"high":  c.High,
			"low":   c.Low,
			"close": c.Close,
		}
	}
	return out
}

// closedTradeLevels больше не используется для отрисовки горизонтальных
// линий: точки входа/выхода уже явно показаны маркерами (стрелка/квадрат)
// на своих свечах в closedTradesToMarkers. Горизонтальная линия Entry через
// весь график только путала (у правого края дня визуально читалась как
// «цена на конец дня»).
func closedTradeLevels(t models.ClosedTrade) []map[string]any {
	return []map[string]any{}
}
