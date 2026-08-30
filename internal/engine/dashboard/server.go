package dashboard

import (
	"context"
	"embed"
	"encoding/json"
	"io/fs"
	"net/http"
	"strings"
	"time"

	"bcs-trading-bot/internal/engine/api"
	"bcs-trading-bot/internal/engine/contract"
	"bcs-trading-bot/internal/models"
)

//go:embed web/*
var webFS embed.FS

// AccountInfo — снимок единого счёта для админки.
type AccountInfo struct {
	Deposit float64   `json:"deposit"`
	Cash    float64   `json:"cash"` // свободный кэш (VirtualExecutor / брокер)
	Updated time.Time `json:"updated_at"`
}

// Options — параметры HTTP-сервера бота (UI + API).
type Options struct {
	Listen   string
	Token    string
	Deposit  float64
	Exec     contract.OrderExecutor
	Reader   contract.TradeReader
	Archives *api.ArchiveStore
	Candles  CandleProvider
}

// Server — HTTP UI и API (live + аналитика + экспорт).
type Server struct {
	hub      *Hub
	listen   string
	token    string
	deposit  float64
	exec     contract.OrderExecutor
	reader   contract.TradeReader
	archives *api.ArchiveStore
	export   *api.ExportService
	candles  CandleProvider
}

func NewServer(hub *Hub, opts Options) (*Server, error) {
	listen := opts.Listen
	if listen == "" {
		listen = "127.0.0.1:8091"
	}
	if err := ValidateHTTPListen(listen, opts.Token); err != nil {
		return nil, err
	}
	s := &Server{
		hub:      hub,
		listen:   listen,
		token:    strings.TrimSpace(opts.Token),
		deposit:  opts.Deposit,
		exec:     opts.Exec,
		reader:   opts.Reader,
		archives: opts.Archives,
		candles:  opts.Candles,
	}
	if opts.Reader != nil {
		s.export = api.NewExportService(opts.Reader)
	}
	return s, nil
}

func (s *Server) ListenAddr() string { return s.listen }

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	webRoot, err := fs.Sub(webFS, "web")
	if err != nil {
		panic(err)
	}
	staticFS, err := fs.Sub(webRoot, "static")
	if err != nil {
		panic(err)
	}
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))

	mux.HandleFunc("GET /{$}", servePage(webRoot, "index.html"))
	mux.HandleFunc("GET /open", servePage(webRoot, "open.html"))
	mux.HandleFunc("GET /day", servePage(webRoot, "day.html"))
	mux.HandleFunc("GET /strategy", servePage(webRoot, "strategy.html"))
	mux.HandleFunc("GET /trades", servePage(webRoot, "trades.html"))
	mux.HandleFunc("GET /export", servePage(webRoot, "export.html"))

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	mux.HandleFunc("GET /account", s.withAuth(s.handleAccount))
	mux.HandleFunc("GET /positions", s.withAuth(s.handlePositions))
	mux.HandleFunc("GET /candles", s.withAuth(s.handleCandles))
	mux.HandleFunc("GET /chart", s.withAuth(s.handleChart))
	mux.HandleFunc("GET /live/account", s.withAuth(s.handleAccount))
	mux.HandleFunc("GET /live/positions", s.withAuth(s.handlePositions))
	mux.HandleFunc("GET /live/candles", s.withAuth(s.handleCandles))
	mux.HandleFunc("GET /live/chart", s.withAuth(s.handleChart))

	mux.HandleFunc("GET /api/summary", s.withAuth(s.handleAPISummary))
	mux.HandleFunc("GET /api/comparison", s.withAuth(s.handleAPIComparison))
	mux.HandleFunc("GET /api/trades", s.withAuth(s.handleAPITrades))
	mux.HandleFunc("GET /api/day-trades", s.withAuth(s.handleAPIDayTrades))
	mux.HandleFunc("GET /api/day-chart", s.withAuth(s.handleAPIDayChart))
	mux.HandleFunc("GET /api/strategy-trades", s.withAuth(s.handleAPIStrategyTrades))
	mux.HandleFunc("GET /api/strategy-trade-chart", s.withAuth(s.handleAPIStrategyTradeChart))
	mux.HandleFunc("GET /api/account-equity", s.withAuth(s.handleAPIAccountEquity))
	mux.HandleFunc("GET /api/date-range", s.withAuth(s.handleAPIDateRange))
	mux.HandleFunc("GET /api/experiments", s.withAuth(s.handleAPIExperiments))
	mux.HandleFunc("GET /api/prompt", s.withAuth(s.handleAPIPrompt))
	mux.HandleFunc("GET /api/export/data", s.withAuth(s.handleExportData))
	mux.HandleFunc("GET /api/archives", s.withAuth(s.handleAPIArchivesList))
	mux.HandleFunc("POST /api/archives", s.withAuth(s.handleAPIArchivesCreate))
	mux.HandleFunc("DELETE /api/archives/{id}", s.withAuth(s.handleAPIArchivesDelete))

	return mux
}

func servePage(fsys fs.FS, name string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data, err := fs.ReadFile(fsys, name)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(data)
	}
}

func (s *Server) handleAccount(w http.ResponseWriter, r *http.Request) {
	info := AccountInfo{
		Deposit: s.deposit,
		Cash:    s.deposit,
		Updated: time.Now().UTC(),
	}
	if s.exec != nil {
		if bal, err := s.exec.GetBalance(r.Context()); err == nil {
			info.Cash = bal
		}
	}
	writeJSON(w, info)
}

func (s *Server) handlePositions(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{
		"updated_at": time.Now().UTC(),
		"positions":  s.hub.Positions(),
	})
}

func (s *Server) handleCandles(w http.ResponseWriter, r *http.Request) {
	ticker := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("ticker")))
	if ticker == "" {
		http.Error(w, "ticker обязателен", http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]any{
		"ticker":  ticker,
		"candles": s.hub.Candles(ticker),
	})
}

func (s *Server) handleChart(w http.ResponseWriter, r *http.Request) {
	ticker := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("ticker")))
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if ticker == "" {
		http.Error(w, "ticker обязателен", http.StatusBadRequest)
		return
	}

	var pos *models.PositionSnapshot
	for _, p := range s.hub.Positions() {
		pp := p
		if id != "" && pp.ID == id {
			pos = &pp
			break
		}
		if id == "" && pp.Ticker == ticker {
			pos = &pp
			break
		}
	}

	candles := s.hub.Candles(ticker)
	payload := BuildOpenChartPayload(candles, pos)
	writeJSON(w, payload)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

// BuildOpenChartPayload — контракт как у optimizer/charts (candles/markers/trades).
func BuildOpenChartPayload(candles []models.Candle, pos *models.PositionSnapshot) map[string]any {
	outCandles := make([]map[string]any, len(candles))
	for i, c := range candles {
		outCandles[i] = map[string]any{
			"time":  c.Timestamp.Unix(),
			"open":  c.Open,
			"high":  c.High,
			"low":   c.Low,
			"close": c.Close,
		}
	}

	markers := []map[string]any{}
	trades := []map[string]any{}
	levels := []map[string]any{}

	if pos != nil {
		isLong := strings.EqualFold(pos.Direction, "BUY")
		entryPos := "belowBar"
		entryColor := "#26a69a"
		entryShape := "arrowUp"
		if !isLong {
			entryPos = "aboveBar"
			entryColor = "#ef5350"
			entryShape = "arrowDown"
		}
		markers = append(markers, map[string]any{
			"time":     pos.OpenedAt.Unix(),
			"position": entryPos,
			"color":    entryColor,
			"shape":    entryShape,
			"text":     "ВХ",
			"size":     2,
		})
		trades = append(trades, map[string]any{
			"index":            1,
			"direction":        strings.ToUpper(pos.Direction),
			"entryTime":        pos.OpenedAt.Unix(),
			"exitTime":         0,
			"entryPrice":       pos.EntryPrice,
			"exitPrice":        pos.LastPrice,
			"pnl":              pos.UnrealizedPnL,
			"entryLabel":       formatOpenTimeMSK(pos.OpenedAt),
			"exitLabel":        "open",
			"closeReason":      "OPEN",
			"closeReasonLabel": "открыта",
		})
		levels = append(levels,
			map[string]any{"price": pos.EntryPrice, "title": "Entry", "color": "#90caf9"},
			map[string]any{"price": pos.StopLoss, "title": "SL", "color": "#ef5350"},
		)
		// TakeProfit == 0 — тейк отключён, линию не рисуем (иначе она уедет в ноль).
		if pos.TakeProfit > 0 {
			levels = append(levels,
				map[string]any{"price": pos.TakeProfit, "title": "TP", "color": "#66bb6a"})
		}
	}

	return map[string]any{
		"candles":  candlesOrEmpty(outCandles),
		"markers":  markers,
		"trades":   trades,
		"levels":   levels,
		"position": pos,
	}
}

var openChartMSK = time.FixedZone("MSK", 3*3600)

func formatOpenTimeMSK(t time.Time) string {
	return t.In(openChartMSK).Format("15:04")
}

func candlesOrEmpty(c []map[string]any) []map[string]any {
	if c == nil {
		return []map[string]any{}
	}
	return c
}

// FetchAccount запрашивает /account у HTTP API бота.
func FetchAccount(ctx context.Context, baseURL string) (AccountInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(baseURL, "/")+"/account", nil)
	if err != nil {
		return AccountInfo{}, err
	}
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return AccountInfo{}, err
	}
	defer resp.Body.Close()
	var info AccountInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return AccountInfo{}, err
	}
	return info, nil
}
