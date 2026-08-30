package live

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"bcs-trading-bot/internal/api"
	"bcs-trading-bot/pkg/interfaces"
	"bcs-trading-bot/internal/models"
)

func (s *Server) requireReader(w http.ResponseWriter) interfaces.TradeReader {
	if s.reader == nil {
		http.Error(w, "хранилище сделок отключено", http.StatusServiceUnavailable)
		return nil
	}
	return s.reader
}

func parseFilter(r *http.Request) models.TradeFilter {
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

func parseExportMode(r *http.Request) (api.ExportMode, error) {
	return api.ParseExportMode(r.URL.Query().Get("mode"))
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

func (s *Server) handleAPISummary(w http.ResponseWriter, r *http.Request) {
	reader := s.requireReader(w)
	if reader == nil {
		return
	}
	summary, err := reader.GetSummary(r.Context(), parseFilter(r))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, summary)
}

func (s *Server) handleAPIComparison(w http.ResponseWriter, r *http.Request) {
	reader := s.requireReader(w)
	if reader == nil {
		return
	}
	rows, err := reader.GetBreakdown(r.Context(), parseFilter(r), "experiment_id")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, rows)
}

func (s *Server) handleAPITrades(w http.ResponseWriter, r *http.Request) {
	reader := s.requireReader(w)
	if reader == nil {
		return
	}
	limit, offset := parseTradesPaging(r)
	result, err := reader.ListClosedTrades(r.Context(), parseFilter(r), limit, offset)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, result)
}

func (s *Server) handleAPIAccountEquity(w http.ResponseWriter, r *http.Request) {
	reader := s.requireReader(w)
	if reader == nil {
		return
	}
	eq, err := reader.GetAccountEquity(r.Context(), parseFilter(r), s.deposit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, eq)
}

func (s *Server) handleAPIDateRange(w http.ResponseWriter, r *http.Request) {
	reader := s.requireReader(w)
	if reader == nil {
		return
	}
	dr, err := reader.GetDateRange(r.Context(), parseFilter(r))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, dr)
}

func (s *Server) handleAPIExperiments(w http.ResponseWriter, r *http.Request) {
	reader := s.requireReader(w)
	if reader == nil {
		return
	}
	ids, err := reader.ListExperimentIDs(r.Context(), models.TradeFilter{})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if ids == nil {
		ids = []string{}
	}
	writeJSON(w, ids)
}

func (s *Server) handleAPIPrompt(w http.ResponseWriter, r *http.Request) {
	if s.export == nil {
		http.Error(w, "хранилище сделок отключено", http.StatusServiceUnavailable)
		return
	}
	mode, err := parseExportMode(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	prompt, err := s.export.BuildPrompt(r.Context(), parseFilter(r), mode)
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

func (s *Server) handleExportData(w http.ResponseWriter, r *http.Request) {
	if s.export == nil {
		http.Error(w, "хранилище сделок отключено", http.StatusServiceUnavailable)
		return
	}
	mode, err := parseExportMode(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	data, err := s.export.BuildExportData(r.Context(), parseFilter(r), mode)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+mode.DataFilename()+`"`)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(data)
}

func (s *Server) handleAPIArchivesList(w http.ResponseWriter, r *http.Request) {
	if s.archives == nil {
		writeJSON(w, []models.ViewArchive{})
		return
	}
	archives, err := s.archives.List()
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

func (s *Server) handleAPIArchivesCreate(w http.ResponseWriter, r *http.Request) {
	if s.archives == nil {
		http.Error(w, "archives не настроены", http.StatusServiceUnavailable)
		return
	}
	var req createArchiveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "некорректный JSON", http.StatusBadRequest)
		return
	}
	archive, err := s.archives.Create(req.DateFrom, req.DateTo, req.Comment)
	if err != nil {
		switch {
		case errors.Is(err, api.ErrArchiveDuplicate):
			http.Error(w, err.Error(), http.StatusConflict)
		case errors.Is(err, api.ErrInvalidDateRange):
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

func (s *Server) handleAPIArchivesDelete(w http.ResponseWriter, r *http.Request) {
	if s.archives == nil {
		http.Error(w, "archives не настроены", http.StatusServiceUnavailable)
		return
	}
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "id обязателен", http.StatusBadRequest)
		return
	}
	if err := s.archives.Delete(id); err != nil {
		if errors.Is(err, api.ErrArchiveNotFound) {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
