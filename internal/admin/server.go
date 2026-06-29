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
	mux.HandleFunc("GET /export", handler.handleExportPage)

	mux.HandleFunc("GET /api/summary", handler.handleAPISummary)
	mux.HandleFunc("GET /api/comparison", handler.handleAPIComparison)
	mux.HandleFunc("GET /api/trades", handler.handleAPITrades)
	mux.HandleFunc("GET /api/prompt", handler.handleAPIPrompt)

	mux.HandleFunc("GET /api/export/data", handler.handleExportData)

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
	"fmtTime": func(t time.Time) string {
		if t.IsZero() {
			return "—"
		}
		return t.Format("02.01 15:04")
	},
	"fmtHold": func(sec int) string {
		return formatHoldDuration(sec)
	},
	"fmtHoldSec": func(sec float64) string {
		return formatHoldDuration(int(sec + 0.5))
	},
	"join": strings.Join,
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
