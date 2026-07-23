package live

import (
	"sync"
	"time"

	"bcs-trading-bot/pkg/models"
)

// OpenPosition — снимок открытой позиции для live API / админки.
type OpenPosition struct {
	ID            string    `json:"id"`
	ExperimentID  string    `json:"experiment_id"`
	Ticker        string    `json:"ticker"`
	Direction     string    `json:"direction"`
	Quantity      int       `json:"quantity"`
	EntryPrice    float64   `json:"entry_price"`
	StopLoss      float64   `json:"stop_loss"`
	TakeProfit    float64   `json:"take_profit"`
	TrailStage    int       `json:"trail_stage"`
	OpenedAt      time.Time `json:"opened_at"`
	LastPrice     float64   `json:"last_price"`
	UnrealizedPnL float64   `json:"unrealized_pnl"`
	RDistance     float64   `json:"r_distance"`
	StepPrice     float64   `json:"step_price"`
}

// PositionSource отдаёт снимок открытой позиции (или nil).
type PositionSource interface {
	Label() string
	Ticker() string
	ExperimentID() string
	SnapshotPosition() *OpenPosition
}

// Hub хранит воркеры и буфер свечей текущего торгового дня.
type Hub struct {
	mu        sync.RWMutex
	sources   []PositionSource
	candles   map[string][]models.Candle
	lastPrice map[string]float64
	dayKey    map[string]string // ticker → YYYY-MM-DD MSK
	loc       *time.Location
}

func NewHub() *Hub {
	loc, err := time.LoadLocation("Europe/Moscow")
	if err != nil {
		loc = time.FixedZone("MSK", 3*3600)
	}
	return &Hub{
		candles:   make(map[string][]models.Candle),
		lastPrice: make(map[string]float64),
		dayKey:    make(map[string]string),
		loc:       loc,
	}
}

func (h *Hub) Register(src PositionSource) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.sources = append(h.sources, src)
}

func (h *Hub) tradingDate(t time.Time) string {
	return t.In(h.loc).Format("2006-01-02")
}

// IngestCandle добавляет/обновляет свечу дня (идемпотентно по timestamp).
func (h *Hub) IngestCandle(c models.Candle) {
	if c.Ticker == "" {
		return
	}
	day := h.tradingDate(c.Timestamp)

	h.mu.Lock()
	defer h.mu.Unlock()

	if h.dayKey[c.Ticker] != day {
		h.dayKey[c.Ticker] = day
		h.candles[c.Ticker] = nil
	}
	h.lastPrice[c.Ticker] = c.Close

	bars := h.candles[c.Ticker]
	if n := len(bars); n > 0 && bars[n-1].Timestamp.Equal(c.Timestamp) {
		bars[n-1] = c
		h.candles[c.Ticker] = bars
		return
	}
	h.candles[c.Ticker] = append(bars, c)
}

// IngestTick обновляет last price.
func (h *Hub) IngestTick(t models.Tick) {
	if t.Ticker == "" || t.Price <= 0 {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.lastPrice[t.Ticker] = t.Price
}

func (h *Hub) Positions() []OpenPosition {
	h.mu.RLock()
	sources := append([]PositionSource(nil), h.sources...)
	last := make(map[string]float64, len(h.lastPrice))
	for k, v := range h.lastPrice {
		last[k] = v
	}
	h.mu.RUnlock()

	out := make([]OpenPosition, 0)
	for _, src := range sources {
		snap := src.SnapshotPosition()
		if snap == nil {
			continue
		}
		if lp, ok := last[snap.Ticker]; ok && lp > 0 {
			snap.LastPrice = lp
			step := snap.StepPrice
			if step <= 0 {
				step = 1
			}
			switch snap.Direction {
			case "BUY":
				snap.UnrealizedPnL = (lp - snap.EntryPrice) * float64(snap.Quantity) * step
			case "SELL":
				snap.UnrealizedPnL = (snap.EntryPrice - lp) * float64(snap.Quantity) * step
			}
		}
		out = append(out, *snap)
	}
	return out
}

func (h *Hub) Candles(ticker string) []models.Candle {
	h.mu.RLock()
	defer h.mu.RUnlock()
	src := h.candles[ticker]
	out := make([]models.Candle, len(src))
	copy(out, src)
	return out
}

func (h *Hub) LastPrice(ticker string) float64 {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.lastPrice[ticker]
}
