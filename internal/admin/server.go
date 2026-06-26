package admin

import (
	"embed"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"strings"
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
	mux.HandleFunc("GET /api/prompt", handler.handleAPIPromptPreview)

	mux.HandleFunc("GET /api/export/ai", handler.handleExportAI)
	mux.HandleFunc("GET /api/export/prompt", handler.handleExportPrompt)
	mux.HandleFunc("GET /api/export/trades.json", handler.handleExportTradesJSON)
	mux.HandleFunc("GET /api/export/trades.csv", handler.handleExportTradesCSV)

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
	"join": strings.Join,
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
