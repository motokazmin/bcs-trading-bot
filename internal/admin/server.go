package admin

import (
	"embed"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"strings"
	"time"
)

//go:embed templates/*.html static/*
var webFS embed.FS

func NewServer(cfg Config, handler *Handler) (*http.Server, error) {
	mux := http.NewServeMux()

	staticFS, err := fs.Sub(webFS, "static")
	if err != nil {
		return nil, err
	}
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))

	mux.HandleFunc("GET /{$}", handler.handleDashboard)
	mux.HandleFunc("GET /trades", handler.handleTrades)
	mux.HandleFunc("GET /open", handler.handleOpen)
	mux.HandleFunc("GET /export", handler.handleExportPage)

	mux.HandleFunc("GET /api/summary", handler.handleAPISummary)
	mux.HandleFunc("GET /api/comparison", handler.handleAPIComparison)
	mux.HandleFunc("GET /api/trades", handler.handleAPITrades)
	mux.HandleFunc("GET /api/account-equity", handler.handleAPIAccountEquity)
	mux.HandleFunc("GET /api/prompt", handler.handleAPIPrompt)

	mux.HandleFunc("GET /api/export/data", handler.handleExportData)

	mux.HandleFunc("GET /api/archives", handler.handleAPIArchivesList)
	mux.HandleFunc("POST /api/archives", handler.handleAPIArchivesCreate)
	mux.HandleFunc("DELETE /api/archives/{id}", handler.handleAPIArchivesDelete)

	mux.HandleFunc("GET /live/", handler.handleLiveProxy)
	mux.HandleFunc("GET /live/account", handler.handleLiveProxy)
	mux.HandleFunc("GET /live/positions", handler.handleLiveProxy)
	mux.HandleFunc("GET /live/candles", handler.handleLiveProxy)
	mux.HandleFunc("GET /live/chart", handler.handleLiveProxy)

	return &http.Server{
		Addr:         cfg.Listen,
		Handler:      mux,
		ReadTimeout:  cfg.ReadTimeout(),
		WriteTimeout: cfg.WriteTimeout(),
	}, nil
}

var templateFuncs = template.FuncMap{
	"printf": fmt.Sprintf,
	"querySuffix": func(q string) string {
		if q == "" {
			return ""
		}
		return "?" + q
	},
	"pageURL": func(path, qs string) template.URL {
		if qs == "" {
			return template.URL(path)
		}
		return template.URL(path + "?" + qs)
	},
	"tradesURL": func(qs string) template.URL {
		if qs == "" {
			return "/trades"
		}
		return template.URL("/trades?" + qs)
	},
	"pnlClass": func(v float64) string {
		if v > 0 {
			return "positive"
		}
		if v < 0 {
			return "negative"
		}
		return ""
	},
	"fmtPct": func(v float64) string {
		return fmt.Sprintf("%.1f%%", v)
	},
	"fmtMoney": func(v float64) string {
		return fmt.Sprintf("%.2f", v)
	},
	"fmtTime": formatAdminTimeMSK,
	"fmtHold": func(sec int) string {
		return formatHoldDuration(sec)
	},
	"fmtHoldSec": func(sec float64) string {
		return formatHoldDuration(int(sec + 0.5))
	},
	"join": strings.Join,
	"add": func(nums ...int) int {
		sum := 0
		for _, n := range nums {
			sum += n
		}
		return sum
	},
}

// formatAdminTimeMSK форматирует время сделки. В БД оно уже хранится в московском
// времени (parseDBTime читает его в зоне Europe/Moscow), поэтому дополнительная
// конвертация не нужна — только форматирование.
func formatAdminTimeMSK(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	return t.Format("02.01 15:04")
}

func formatHoldDuration(sec int) string {
	if sec <= 0 {
		return "—"
	}
	if sec < 60 {
		return fmt.Sprintf("%dс", sec)
	}
	if sec < 3600 {
		return fmt.Sprintf("%dм", sec/60)
	}
	h := sec / 3600
	m := (sec % 3600) / 60
	if m == 0 {
		return fmt.Sprintf("%dч", h)
	}
	return fmt.Sprintf("%dч %02dм", h, m)
}

func renderTemplate(w http.ResponseWriter, name string, data any) {
	tmpl, err := template.New("layout.html").Funcs(templateFuncs).ParseFS(webFS,
		"templates/layout.html",
		"templates/filters.html",
		"templates/"+name,
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(w, "layout.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
