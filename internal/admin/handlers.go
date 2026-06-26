package admin

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"bcs-trading-bot/pkg/interfaces"
	"bcs-trading-bot/pkg/models"
)

// Handler обслуживает HTTP-запросы админки.
type Handler struct {
	reader interfaces.TradeReader
	export *ExportService
}

func NewHandler(reader interfaces.TradeReader) *Handler {
	return &Handler{
		reader: reader,
		export: NewExportService(reader),
	}
}

func (h *Handler) parseFilter(r *http.Request) models.TradeFilter {
	q := r.URL.Query()
	return models.TradeFilter{
		ExperimentID: strings.TrimSpace(q.Get("experiment_id")),
		Ticker:       strings.TrimSpace(strings.ToUpper(q.Get("ticker"))),
		TradingMode:  strings.TrimSpace(q.Get("trading_mode")),
		RunID:        strings.TrimSpace(q.Get("run_id")),
		DateFrom:     strings.TrimSpace(q.Get("date_from")),
		DateTo:       strings.TrimSpace(q.Get("date_to")),
		CloseReason:  strings.TrimSpace(q.Get("close_reason")),
	}
}

func (h *Handler) handleExportAI(w http.ResponseWriter, r *http.Request) {
	bundle, err := h.export.BuildAIExport(r.Context(), h.parseFilter(r))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="export-ai.json"`)
	writeJSONBytes(w, bundle)
}

func (h *Handler) handleExportPrompt(w http.ResponseWriter, r *http.Request) {
	bundle, err := h.export.BuildAIExport(r.Context(), h.parseFilter(r))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="ai-prompt.md"`)
	_, _ = w.Write([]byte(RenderPromptMarkdown(bundle)))
}

func (h *Handler) handleExportTradesJSON(w http.ResponseWriter, r *http.Request) {
	trades, err := h.export.listAllTrades(r.Context(), h.parseFilter(r))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="trades.json"`)
	writeJSONBytes(w, trades)
}

func (h *Handler) handleExportTradesCSV(w http.ResponseWriter, r *http.Request) {
	trades, err := h.export.listAllTrades(r.Context(), h.parseFilter(r))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="trades.csv"`)

	cw := csv.NewWriter(w)
	_ = cw.Write([]string{
		"experiment_id", "stop_mode", "ticker", "direction", "trading_date",
		"entry_price", "exit_price", "gross_pnl", "pnl_r", "close_reason",
		"trail_stage", "hold_seconds", "opened_at", "closed_at", "run_id",
	})
	for _, t := range trades {
		_ = cw.Write([]string{
			t.ExperimentID, t.StopMode, t.Ticker, t.Direction, t.TradingDate,
			fmtFloat(t.EntryPrice), fmtFloat(t.ExitPrice), fmtFloat(t.GrossPnL), fmtFloat(t.PnLR),
			t.CloseReason, strconv.Itoa(t.TrailStage), strconv.Itoa(t.HoldSeconds),
			t.OpenedAt.Format(time.RFC3339), t.ClosedAt.Format(time.RFC3339), t.RunID,
		})
	}
	cw.Flush()
}

func (h *Handler) handleAPISummary(w http.ResponseWriter, r *http.Request) {
	summary, err := h.reader.GetSummary(r.Context(), h.parseFilter(r))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, summary)
}

func (h *Handler) handleAPIComparison(w http.ResponseWriter, r *http.Request) {
	rows, err := h.reader.GetBreakdown(r.Context(), h.parseFilter(r), "experiment_id")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, rows)
}

func (h *Handler) handleAPITrades(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if limit <= 0 {
		limit = 50
	}
	result, err := h.reader.ListClosedTrades(r.Context(), h.parseFilter(r), limit, offset)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, result)
}

func (h *Handler) handleAPIPromptPreview(w http.ResponseWriter, r *http.Request) {
	bundle, err := h.export.BuildAIExport(r.Context(), h.parseFilter(r))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"prompt": bundle.Prompt})
}

type pageData struct {
	Title       string
	ActiveNav   string
	FilterQuery string
	Summary     models.TradeSummary
	Comparison  []models.BreakdownRow
	Trades      models.TradeListResult
	DateRange   models.DateRange
	Experiments []string
}

func (h *Handler) buildPageData(ctx context.Context, r *http.Request, title, nav string) (pageData, error) {
	f := h.parseFilter(r)
	summary, err := h.reader.GetSummary(ctx, f)
	if err != nil {
		return pageData{}, err
	}
	comparison, err := h.reader.GetBreakdown(ctx, f, "experiment_id")
	if err != nil {
		return pageData{}, err
	}
	dr, err := h.reader.GetDateRange(ctx, f)
	if err != nil {
		return pageData{}, err
	}
	experiments, err := h.reader.ListExperimentIDs(ctx, models.TradeFilter{})
	if err != nil {
		return pageData{}, err
	}
	return pageData{
		Title:       title,
		ActiveNav:   nav,
		FilterQuery: r.URL.RawQuery,
		Summary:     summary,
		Comparison:  comparison,
		DateRange:   dr,
		Experiments: experiments,
	}, nil
}

func (h *Handler) handleDashboard(w http.ResponseWriter, r *http.Request) {
	data, err := h.buildPageData(r.Context(), r, "Дашборд", "dashboard")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	renderTemplate(w, "dashboard.html", data)
}

func (h *Handler) handleTrades(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if limit <= 0 {
		limit = 50
	}
	trades, err := h.reader.ListClosedTrades(r.Context(), h.parseFilter(r), limit, offset)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	data, err := h.buildPageData(r.Context(), r, "Сделки", "trades")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	data.Trades = trades
	renderTemplate(w, "trades.html", data)
}

func (h *Handler) handleExportPage(w http.ResponseWriter, r *http.Request) {
	data, err := h.buildPageData(r.Context(), r, "Экспорт для ИИ", "export")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	renderTemplate(w, "export.html", data)
}

func fmtFloat(v float64) string {
	return strconv.FormatFloat(v, 'f', 4, 64)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	writeJSONBytes(w, v)
}

func writeJSONBytes(w http.ResponseWriter, v any) {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
