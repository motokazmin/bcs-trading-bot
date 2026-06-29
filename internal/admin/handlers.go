package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

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

func (h *Handler) parseExportMode(r *http.Request) (ExportMode, error) {
	return ParseExportMode(r.URL.Query().Get("mode"))
}

func (h *Handler) handleExportData(w http.ResponseWriter, r *http.Request) {
	mode, err := h.parseExportMode(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	data, err := h.export.BuildExportData(r.Context(), h.parseFilter(r), mode)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+mode.DataFilename()+`"`)
	writeJSONBytes(w, data)
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

func (h *Handler) handleAPIPrompt(w http.ResponseWriter, r *http.Request) {
	mode, err := h.parseExportMode(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	prompt, err := h.export.BuildPrompt(r.Context(), h.parseFilter(r), mode)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{
		"mode":     string(mode),
		"prompt":   prompt,
		"data_file": mode.DataFilename(),
	})
}

type pageData struct {
	Title       string
	ActiveNav   string
	FilterQuery string
	Filter      models.TradeFilter
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
		Filter:      f,
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
