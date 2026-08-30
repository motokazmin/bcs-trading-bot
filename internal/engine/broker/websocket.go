package broker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"bcs-trading-bot/internal/logx"
	"bcs-trading-bot/internal/models"

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

// RouteKey — подписка на свечи конкретного тикера в конкретном таймфрейме.
// Один тикер может иметь несколько одновременных подписок с разным
// Timeframe (разные стратегии, разные таймфреймы) — см. ADR 0001 и
// internal/datafeed.
type RouteKey struct {
	Ticker    string
	Timeframe string
}

// tickerRoutes — вспомогательная развёртка routes для рассылки котировок:
// котировки (Quotes) не привязаны к таймфрейму свечей, поэтому тик должен
// доходить до всех маршрутов тикера независимо от того, на какой Timeframe
// подписан конкретный маршрут.
func tickerRoutes(routes map[RouteKey][]WorkerRoutes) map[string][]WorkerRoutes {
	out := make(map[string][]WorkerRoutes)
	for key, wr := range routes {
		out[key.Ticker] = append(out[key.Ticker], wr...)
	}
	return out
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

// SubscribeToCandles подключается к WebSocket БКС и передаёт свечи в candleChan
// на таймфрейме по умолчанию (c.candleTimeFrame). Оставлено для smoke-теста
// и других сценариев с одним тикером/одним таймфреймом.
func (c *BCSClient) SubscribeToCandles(ctx context.Context, ticker string, candleChan chan<- models.Candle) error {
	timeframe := c.candleTimeFrame
	if timeframe == "" {
		timeframe = CandleTimeFrame
	}
	routes := map[RouteKey][]WorkerRoutes{
		{Ticker: ticker, Timeframe: timeframe}: {{CandleChan: candleChan}},
	}
	return c.runMarketDataSession(ctx, routes)
}

// SubscribeMarketDataFanOut подписывается на свечи и котировки для набора
// маршрутов, каждый ключ которых — пара (тикер, таймфрейм). Один тикер может
// одновременно иметь несколько маршрутов с разными таймфреймами (разные
// стратегии) — все они уходят на одно WebSocket-соединение отдельными
// subscribe-сообщениями с нужным TimeFrame.
func (c *BCSClient) SubscribeMarketDataFanOut(ctx context.Context, routes map[RouteKey][]WorkerRoutes) error {
	if len(routes) == 0 {
		return fmt.Errorf("список маршрутов пуст")
	}

	backoff := time.Second
	const maxBackoff = 60 * time.Second

	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		err := c.runMarketDataSession(ctx, routes)
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

func (c *BCSClient) runMarketDataSession(ctx context.Context, routes map[RouteKey][]WorkerRoutes) error {
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

	// Группируем тикеры по таймфрейму: сообщение подписки на свечи несёт один
	// TimeFrame на весь список Instruments, поэтому на каждый уникальный
	// таймфрейм нужно своё subscribe-сообщение (см. RouteKey/ADR 0001).
	tickersByTimeframe := make(map[string][]string)
	allTickersSet := make(map[string]struct{})
	for key := range routes {
		timeframe := key.Timeframe
		if timeframe == "" {
			timeframe = CandleTimeFrame
		}
		tickersByTimeframe[timeframe] = append(tickersByTimeframe[timeframe], key.Ticker)
		allTickersSet[key.Ticker] = struct{}{}
	}

	for timeframe, tickers := range tickersByTimeframe {
		instruments := make([]wsInstrument, len(tickers))
		for i, ticker := range tickers {
			instruments[i] = wsInstrument{ClassCode: classCode, Ticker: ticker}
		}
		candleSub := wsSubscribeRequest{
			SubscribeType: 0,
			DataType:      wsDataTypeCandles,
			TimeFrame:     timeframe,
			Instruments:   instruments,
		}
		if err := conn.WriteJSON(candleSub); err != nil {
			return fmt.Errorf("ошибка подписки на свечи (%s): %w", timeframe, err)
		}
		logx.WS("подписка %d инструмент(ов) %v — свечи %s", len(tickers), tickers, timeframe)
	}

	allTickers := make([]wsInstrument, 0, len(allTickersSet))
	for ticker := range allTickersSet {
		allTickers = append(allTickers, wsInstrument{ClassCode: classCode, Ticker: ticker})
	}
	quoteSub := wsSubscribeRequest{
		SubscribeType: 0,
		DataType:      wsDataTypeQuotes,
		Instruments:   allTickers,
	}
	if err := conn.WriteJSON(quoteSub); err != nil {
		return fmt.Errorf("ошибка подписки на котировки: %w", err)
	}
	logx.WS("подписка %d инструмент(ов) — котировки", len(allTickers))

	tickRoutes := tickerRoutes(routes)

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

		if err := c.dispatchMarketMessage(ctx, raw, routes, tickRoutes); err != nil {
			return err
		}
	}
}

func isUnauthorizedWSError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "HTTP 401")
}

// dispatchMarketMessage маршрутизирует входящее сообщение. Свечи (CandleStick)
// маршрутизируются по (Ticker, TimeFrame) — так несколько подписок на один
// тикер с разным таймфреймом не путают друг друга. Котировки (Quotes) не
// несут TimeFrame и рассылаются во все маршруты тикера через tickRoutes.
func (c *BCSClient) dispatchMarketMessage(ctx context.Context, raw []byte, routes map[RouteKey][]WorkerRoutes, tickRoutes map[string][]WorkerRoutes) error {
	var header struct {
		ResponseType string `json:"responseType"`
		Ticker       string `json:"ticker"`
		TimeFrame    string `json:"timeFrame"`
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

	switch header.ResponseType {
	case "CandleStick":
		timeframe := header.TimeFrame
		if timeframe == "" {
			timeframe = CandleTimeFrame
		}
		routeList, ok := routes[RouteKey{Ticker: header.Ticker, Timeframe: timeframe}]
		if !ok {
			return nil
		}
		for _, route := range routeList {
			if err := c.dispatchCandle(ctx, raw, route); err != nil {
				return err
			}
		}
	case "Quotes":
		routeList, ok := tickRoutes[header.Ticker]
		if !ok {
			return nil
		}
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
		ts = time.Now().UTC()
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
		ts, err = time.Parse(time.RFC3339Nano, value)
		if err != nil {
			return time.Time{}, err
		}
	}
	return ts.UTC(), nil
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
