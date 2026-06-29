package bcs

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"bcs-trading-bot/pkg/logx"
	"bcs-trading-bot/pkg/models"

	"github.com/gorilla/websocket"
)

const (
	MarketDataWSURL  = "wss://ws.broker.ru/trade-api-market-data-connector/api/v1/market-data/ws"
	DefaultClassCode = "TQBR"
	CandleTimeFrame  = "M5"

	wsDataTypeCandles = 1
	wsDataTypeQuotes  = 3

	wsReadDeadlineActive = 90 * time.Second
	wsReadDeadlineQuiet  = 60 * time.Minute

	wsQuietStartMinutes = 23*60 + 50 // 23:50 МСК
	wsQuietEndMinutes   = 7 * 60     // 07:00 МСК
)

var moscowLoc = func() *time.Location {
	loc, err := time.LoadLocation("Europe/Moscow")
	if err != nil {
		return time.FixedZone("MSK", 3*3600)
	}
	return loc
}()

// isWSQuietPeriod — ночное окно без рыночных данных (после вечерней сессии до утра).
func isWSQuietPeriod(now time.Time) bool {
	m := now.In(moscowLoc).Hour()*60 + now.In(moscowLoc).Minute()
	return m >= wsQuietStartMinutes || m < wsQuietEndMinutes
}

func wsReadDeadline(now time.Time) time.Duration {
	if isWSQuietPeriod(now) {
		return wsReadDeadlineQuiet
	}
	return wsReadDeadlineActive
}

// WorkerRoutes — каналы доставки рыночных данных одному воркеру.
type WorkerRoutes struct {
	CandleChan chan<- models.Candle
	TickChan   chan<- models.Tick
}

type wsInstrument struct {
	ClassCode string `json:"classCode"`
	Ticker    string `json:"ticker"`
}

type wsSubscribeRequest struct {
	SubscribeType int            `json:"subscribeType"`
	DataType      int            `json:"dataType"`
	TimeFrame     string         `json:"timeFrame,omitempty"`
	Instruments   []wsInstrument `json:"instruments"`
}

type candleWSMessage struct {
	ResponseType string  `json:"responseType"`
	Ticker       string  `json:"ticker"`
	ClassCode    string  `json:"classCode"`
	TimeFrame    string  `json:"timeFrame"`
	Open         float64 `json:"open"`
	Close        float64 `json:"close"`
	High         float64 `json:"high"`
	Low          float64 `json:"low"`
	Volume       float64 `json:"volume"`
	DateTime     string  `json:"dateTime"`
	Errors       []struct {
		Message string `json:"message"`
		Code    string `json:"code"`
	} `json:"errors"`
}

type quoteWSMessage struct {
	ResponseType string  `json:"responseType"`
	Ticker       string  `json:"ticker"`
	ClassCode    string  `json:"classCode"`
	Last         float64 `json:"last"`
	DateTime     string  `json:"dateTime"`
	Errors       []struct {
		Message string `json:"message"`
		Code    string `json:"code"`
	} `json:"errors"`
}

// SubscribeToCandles подключается к WebSocket БКС и передаёт свечи в candleChan.
func (c *BCSClient) SubscribeToCandles(ctx context.Context, ticker string, candleChan chan<- models.Candle) error {
	routes := map[string][]WorkerRoutes{
		ticker: {{CandleChan: candleChan}},
	}
	return c.runMarketDataSession(ctx, []string{ticker}, routes)
}

// SubscribeCandlesFanOut — совместимость: только свечи без тиков.
func (c *BCSClient) SubscribeCandlesFanOut(ctx context.Context, routes map[string]chan<- models.Candle) error {
	wr := make(map[string][]WorkerRoutes, len(routes))
	for ticker, ch := range routes {
		wr[ticker] = []WorkerRoutes{{CandleChan: ch}}
	}
	return c.SubscribeMarketDataFanOut(ctx, wr)
}

// SubscribeMarketDataFanOut подписывается на свечи и котировки для всех тикеров.
// На один тикер может быть несколько маршрутов (параллельные эксперименты).
func (c *BCSClient) SubscribeMarketDataFanOut(ctx context.Context, routes map[string][]WorkerRoutes) error {
	if len(routes) == 0 {
		return fmt.Errorf("список маршрутов пуст")
	}

	tickers := make([]string, 0, len(routes))
	for ticker := range routes {
		tickers = append(tickers, ticker)
	}

	backoff := time.Second
	const maxBackoff = 60 * time.Second

	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		err := c.runMarketDataSession(ctx, tickers, routes)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}

			if isUnauthorizedWSError(err) {
				logx.WS("токен протух, переавторизация...")
				if authErr := c.Connect(ctx); authErr != nil {
					logx.WS("переавторизация не удалась: %v, backoff %s", authErr, backoff)
				} else {
					logx.WS("переавторизация успешна, переподключение")
					backoff = time.Second
					continue
				}
			} else {
				logx.WS("рыночные данные: %v, переподключение через %s", err, backoff)
			}
		} else {
			backoff = time.Second
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}

		backoff *= 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
}

func (c *BCSClient) runMarketDataSession(ctx context.Context, tickers []string, routes map[string][]WorkerRoutes) error {
	token := c.AccessToken()
	if token == "" {
		return fmt.Errorf("клиент не авторизован")
	}

	classCode := c.classCode
	if classCode == "" {
		classCode = DefaultClassCode
	}

	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+token)

	dialer := websocket.Dialer{HandshakeTimeout: 30 * time.Second}
	conn, resp, err := dialer.DialContext(ctx, MarketDataWSURL, headers)
	if err != nil {
		return formatWSDialError(err, resp)
	}
	defer conn.Close()

	instruments := make([]wsInstrument, len(tickers))
	for i, ticker := range tickers {
		instruments[i] = wsInstrument{ClassCode: classCode, Ticker: ticker}
	}

	timeFrame := c.candleTimeFrame
	if timeFrame == "" {
		timeFrame = CandleTimeFrame
	}

	candleSub := wsSubscribeRequest{
		SubscribeType: 0,
		DataType:      wsDataTypeCandles,
		TimeFrame:     timeFrame,
		Instruments:   instruments,
	}
	if err := conn.WriteJSON(candleSub); err != nil {
		return fmt.Errorf("ошибка подписки на свечи: %w", err)
	}

	quoteSub := wsSubscribeRequest{
		SubscribeType: 0,
		DataType:      wsDataTypeQuotes,
		Instruments:   instruments,
	}
	if err := conn.WriteJSON(quoteSub); err != nil {
		return fmt.Errorf("ошибка подписки на котировки: %w", err)
	}

	logx.WS("подписка %d инструмент(ов) %v — свечи %s + котировки", len(tickers), tickers, timeFrame)

	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(wsReadDeadline(time.Now())))
	})

	pingTicker := time.NewTicker(30 * time.Second)
	defer pingTicker.Stop()

	go func() {
		for {
			select {
			case <-pingTicker.C:
				if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if err := conn.SetReadDeadline(time.Now().Add(wsReadDeadline(time.Now()))); err != nil {
			return err
		}

		_, raw, err := conn.ReadMessage()
		if err != nil {
			return fmt.Errorf("ошибка чтения: %w", err)
		}

		if err := c.dispatchMarketMessage(ctx, raw, routes); err != nil {
			return err
		}
	}
}

func isUnauthorizedWSError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "HTTP 401")
}

func (c *BCSClient) dispatchMarketMessage(ctx context.Context, raw []byte, routes map[string][]WorkerRoutes) error {
	var header struct {
		ResponseType string `json:"responseType"`
		Ticker       string `json:"ticker"`
		Errors       []struct {
			Message string `json:"message"`
			Code    string `json:"code"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(raw, &header); err != nil {
		logx.WS("не удалось разобрать сообщение: %v", err)
		return nil
	}

	if len(header.Errors) > 0 {
		logx.WS("ошибка от сервера [%s]: %s (%s)",
			header.Ticker, header.Errors[0].Message, header.Errors[0].Code)
		return nil
	}

	routeList, ok := routes[header.Ticker]
	if !ok {
		return nil
	}

	switch header.ResponseType {
	case "CandleStick":
		for _, route := range routeList {
			if err := c.dispatchCandle(ctx, raw, route); err != nil {
				return err
			}
		}
	case "Quotes":
		for _, route := range routeList {
			if err := c.dispatchQuote(ctx, raw, route); err != nil {
				return err
			}
		}
	}

	return nil
}

func (c *BCSClient) dispatchCandle(ctx context.Context, raw []byte, route WorkerRoutes) error {
	if route.CandleChan == nil {
		return nil
	}

	var msg candleWSMessage
	if err := json.Unmarshal(raw, &msg); err != nil {
		return nil
	}

	ts, err := parseWSDateTime(msg.DateTime)
	if err != nil {
		logx.WS("неверный dateTime %q: %v", msg.DateTime, err)
		return nil
	}

	candle := models.Candle{
		Ticker:    msg.Ticker,
		Open:      msg.Open,
		High:      msg.High,
		Low:       msg.Low,
		Close:     msg.Close,
		Volume:    int64(msg.Volume),
		Timestamp: ts,
	}

	select {
	case route.CandleChan <- candle:
	case <-ctx.Done():
		return ctx.Err()
	}
	return nil
}

func (c *BCSClient) dispatchQuote(ctx context.Context, raw []byte, route WorkerRoutes) error {
	if route.TickChan == nil {
		return nil
	}

	var msg quoteWSMessage
	if err := json.Unmarshal(raw, &msg); err != nil {
		return nil
	}

	if msg.Last <= 0 {
		return nil
	}

	ts, err := parseWSDateTime(msg.DateTime)
	if err != nil {
		ts = time.Now()
	}

	tick := models.Tick{
		Ticker:    msg.Ticker,
		Price:     msg.Last,
		Timestamp: ts,
	}

	select {
	case route.TickChan <- tick:
	case <-ctx.Done():
		return ctx.Err()
	}
	return nil
}

func parseWSDateTime(value string) (time.Time, error) {
	ts, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Parse(time.RFC3339Nano, value)
	}
	return ts, nil
}

func formatWSDialError(err error, resp *http.Response) error {
	if resp == nil {
		return fmt.Errorf("ошибка подключения WebSocket: %w", err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	return fmt.Errorf("ошибка подключения WebSocket (HTTP %d): %w, ответ: %s",
		resp.StatusCode, err, string(body))
}
