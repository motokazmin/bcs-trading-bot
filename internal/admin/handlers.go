package admin

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"bcs-trading-bot/internal/live"
	"bcs-trading-bot/pkg/interfaces"
	"bcs-trading-bot/pkg/models"
)

// Handler обслуживает HTTP-запросы админки.
type Handler struct {
	reader     interfaces.TradeReader
	export     *ExportService
	archives   *ArchiveStore
	botLiveURL string
}

func NewHandler(reader interfaces.TradeReader, archives *ArchiveStore, botLiveURL string) *Handler {
	return &Handler{
		reader:     reader,
		export:     NewExportService(reader),
		archives:   archives,
		botLiveURL: strings.TrimRight(botLiveURL, "/"),
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
	limit, offset := parseTradesPaging(r)
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
		"mode":      string(mode),
		"prompt":    prompt,
		"data_file": mode.DataFilename(),
	})
}

type pageLink struct {
	Page    int
	Offset  int
	QS      string
	Current bool
}

type perPageOption struct {
	Value   int
	QS      string
	Current bool
}

type tradesPagination struct {
	Limit          int
	Offset         int
	Total          int
	From           int
	To             int
	HasPrev        bool
	HasNext        bool
	PrevQS         string
	NextQS         string
	FirstQS        string
	LastQS         string
	CurrentPage    int
	TotalPages     int
	Pages          []pageLink
	PerPageOptions []perPageOption
	Empty          bool // Total > 0, но на этой странице нет строк (протухший offset)
}

type pageData struct {
	Title            string
	ActiveNav        string
	FilterQuery      string
	Filter           models.TradeFilter
	Summary          models.TradeSummary
	Comparison       []models.BreakdownRow
	Trades           models.TradeListResult
	TradesPagination tradesPagination
	DateRange        models.DateRange
	Experiments      []string
	AccountEquity models.AccountEquity
	BotLiveURL    string
	BotOnline     bool
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
	deposit := h.fetchAccountDeposit(ctx)
	equity, err := h.reader.GetAccountEquity(ctx, f, deposit)
	if err != nil {
		return pageData{}, err
	}
	return pageData{
		Title:         title,
		ActiveNav:     nav,
		FilterQuery:   r.URL.RawQuery,
		Filter:        f,
		Summary:       summary,
		Comparison:    comparison,
		DateRange:     dr,
		Experiments:   experiments,
		AccountEquity: equity,
		BotLiveURL:    h.botLiveURL,
	}, nil
}

// fetchAccountDeposit спрашивает депозит у live API бота; 0 если бот недоступен.
func (h *Handler) fetchAccountDeposit(ctx context.Context) float64 {
	if h.botLiveURL == "" {
		return 0
	}
	info, err := live.FetchAccount(ctx, h.botLiveURL)
	if err != nil {
		return 0
	}
	return info.Deposit
}

func (h *Handler) handleDashboard(w http.ResponseWriter, r *http.Request) {
	data, err := h.buildPageData(r.Context(), r, "Дашборд", "dashboard")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	renderTemplate(w, "dashboard.html", data)
}

func parseTradesPaging(r *http.Request) (limit, offset int) {
	limit, _ = strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ = strconv.Atoi(r.URL.Query().Get("offset"))
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}

func tradesPageQuery(values url.Values, limit, offset int) string {
	q := url.Values{}
	for k, vs := range values {
		for _, v := range vs {
			q.Add(k, v)
		}
	}
	q.Set("limit", strconv.Itoa(limit))
	if offset <= 0 {
		q.Del("offset")
	} else {
		q.Set("offset", strconv.Itoa(offset))
	}
	return q.Encode()
}

var perPageChoices = []int{25, 50, 100, 200}

func buildTradesPagination(r *http.Request, limit, offset int, result models.TradeListResult) tradesPagination {
	total := result.Total

	totalPages := 0
	if total > 0 {
		totalPages = (total + limit - 1) / limit
	}
	currentPage := offset/limit + 1
	if totalPages > 0 && currentPage > totalPages {
		currentPage = totalPages
	}
	if currentPage < 1 {
		currentPage = 1
	}

	from, to := 0, 0
	if len(result.Trades) > 0 {
		from = offset + 1
		to = offset + len(result.Trades)
	}

	prevOffset := offset - limit
	if prevOffset < 0 {
		prevOffset = 0
	}
	nextOffset := offset + limit
	lastOffset := (totalPages - 1) * limit
	if lastOffset < 0 {
		lastOffset = 0
	}

	q := r.URL.Query()
	qsFor := func(off int) string { return tradesPageQuery(q, limit, off) }

	// Окно номеров страниц: максимум 7 штук вокруг текущей.
	var pages []pageLink
	if totalPages > 0 {
		const window = 7
		start := currentPage - window/2
		if start < 1 {
			start = 1
		}
		end := start + window - 1
		if end > totalPages {
			end = totalPages
			start = end - window + 1
			if start < 1 {
				start = 1
			}
		}
		for p := start; p <= end; p++ {
			off := (p - 1) * limit
			pages = append(pages, pageLink{Page: p, Offset: off, QS: qsFor(off), Current: p == currentPage})
		}
	}

	var perPage []perPageOption
	for _, v := range perPageChoices {
		perPage = append(perPage, perPageOption{
			Value:   v,
			QS:      tradesPageQuery(q, v, 0), // смена размера страницы всегда возвращает на страницу 1
			Current: v == limit,
		})
	}

	return tradesPagination{
		Limit:          limit,
		Offset:         offset,
		Total:          total,
		From:           from,
		To:             to,
		HasPrev:        offset > 0,
		HasNext:        offset+len(result.Trades) < total,
		PrevQS:         qsFor(prevOffset),
		NextQS:         qsFor(nextOffset),
		FirstQS:        qsFor(0),
		LastQS:         qsFor(lastOffset),
		CurrentPage:    currentPage,
		TotalPages:     totalPages,
		Pages:          pages,
		PerPageOptions: perPage,
		Empty:          total > 0 && len(result.Trades) == 0,
	}
}

func (h *Handler) handleTrades(w http.ResponseWriter, r *http.Request) {
	limit, offset := parseTradesPaging(r)
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
	data.TradesPagination = buildTradesPagination(r, limit, offset, trades)
	renderTemplate(w, "trades.html", data)
}

func (h *Handler) handleAPIAccountEquity(w http.ResponseWriter, r *http.Request) {
	eq, err := h.reader.GetAccountEquity(r.Context(), h.parseFilter(r), h.fetchAccountDeposit(r.Context()))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, eq)
}

func (h *Handler) handleOpen(w http.ResponseWriter, r *http.Request) {
	data, err := h.buildPageData(r.Context(), r, "Открытые", "open")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	data.BotOnline = h.botLiveURL != ""
	renderTemplate(w, "open.html", data)
}

// ProxyLive проксирует запросы к live API бота (обход CORS для локальной админки).
func (h *Handler) handleLiveProxy(w http.ResponseWriter, r *http.Request) {
	if h.botLiveURL == "" {
		http.Error(w, "bot live URL не задан", http.StatusServiceUnavailable)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/live")
	if path == "" {
		path = "/"
	}
	target := h.botLiveURL + path
	if r.URL.RawQuery != "" {
		target += "?" + r.URL.RawQuery
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, target, nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		http.Error(w, "бот недоступен: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	for k, vv := range resp.Header {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

func (h *Handler) handleAPIArchivesList(w http.ResponseWriter, r *http.Request) {
	archives, err := h.archives.List()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if archives == nil {
		archives = []models.ViewArchive{}
	}
	writeJSON(w, archives)
}

type createArchiveRequest struct {
	DateFrom string `json:"date_from"`
	DateTo   string `json:"date_to"`
	Comment  string `json:"comment"`
}

func (h *Handler) handleAPIArchivesCreate(w http.ResponseWriter, r *http.Request) {
	var req createArchiveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "некорректный JSON", http.StatusBadRequest)
		return
	}
	archive, err := h.archives.Create(req.DateFrom, req.DateTo, req.Comment)
	if err != nil {
		switch {
		case errors.Is(err, ErrArchiveDuplicate):
			http.Error(w, err.Error(), http.StatusConflict)
		case errors.Is(err, ErrInvalidDateRange):
			http.Error(w, err.Error(), http.StatusBadRequest)
		default:
			if strings.Contains(err.Error(), "date_from:") || strings.Contains(err.Error(), "date_to:") {
				http.Error(w, err.Error(), http.StatusBadRequest)
			} else {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
		}
		return
	}
	w.WriteHeader(http.StatusCreated)
	writeJSON(w, archive)
}

func (h *Handler) handleAPIArchivesDelete(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/archives/")
	if id == "" || strings.Contains(id, "/") {
		http.Error(w, "id обязателен", http.StatusBadRequest)
		return
	}
	if err := h.archives.Delete(id); err != nil {
		if errors.Is(err, ErrArchiveNotFound) {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
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
