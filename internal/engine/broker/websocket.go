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

// Market-data WebSocket БКС (trade-api) — специфика, которую легко забыть.
//
// Живой поток: текущие свечи по мере закрытия бара + котировки (тики),
// начиная "с этого момента". Историю WS не отдаёт — за прошлым идём в REST
// (internal/engine/marketdata/fetch.go).
//
// Соединение и авторизация
//   - Тот же сервис market-data-connector, что и REST candles-chart в
//     internal/engine/marketdata/fetch.go — но здесь поток, а не запрос-ответ.
//   - Токен уходит Bearer-заголовком при рукопожатии (DialContext), не в
//     сообщении. Живёт минуты; протух — рукопожатие падает с HTTP 401.
//     Отдельного кода в ответе нет, ловим по подстроке "HTTP 401" в тексте
//     ошибки (isUnauthorizedWSError) → c.Connect и переподключение.
//
// Подписка — это сообщения в открытый сокет, не query-параметры:
//   - subscribeType: 0 = подписаться. Ненулевое — отписка, в боте не нужна.
//   - dataType: 1 = свечи (wsDataTypeCandles), 3 = котировки (wsDataTypeQuotes).
//     2 (стакан) не трогаем.
//   - Свечи: одно subscribe-сообщение несёт ОДИН timeFrame на весь список
//     instruments. N разных таймфреймов → N сообщений (см. tickersByTimeframe
//     и RouteKey/ADR 0001).
//   - Котировки: поля timeFrame нет вообще (omitempty), одно сообщение на все
//     инструменты. Тик приходит без таймфрейма → раздаём во все маршруты
//     тикера независимо от Timeframe маршрута (tickerRoutes).
//   - instruments: пары {classCode, ticker}. classCode по умолчанию TQBR —
//     основные торги акциями MOEX.
//
// Входящие сообщения
//   - JSON, тип в поле responseType: "CandleStick" | "Quotes".
//   - Ошибки приходят не отдельным каналом, а полем errors[] внутри обычного
//     сообщения (и на уровне сообщения, и по тикеру). Пустой responseType с
//     непустым errors — это отлуп подписки, а не данные.
//   - timeFrame в ответе на свечу может отсутствовать → считаем CandleTimeFrame (M5).
//   - dateTime: RFC3339 либо RFC3339Nano, без гарантии какой именно;
//     нормализуем в UTC. У котировки может не распарситься — подставляем now.
//   - Свеча: open/high/low/close/volume — float64; volume приходит дробным,
//     приводим к int64.
//   - Котировка: цена в поле last. last <= 0 — не сделка, а заглушка
//     "данных нет", пропускаем.
//
// Живучесть соединения
//   - Сервер свой ping не шлёт. Клиент сам шлёт PingMessage каждые 30с, по
//     pong двигает read deadline. Без этого сервер молча роняет коннект.
//   - Read deadline: 90с в рынок, 60мин в ночное окно 23:50–07:00 МСК — ночью
//     данных нет совсем, обычный дедлайн убил бы живой коннект и запустил
//     бесполезный цикл реконнектов.
//   - Реконнект: экспоненциальный backoff 1s→60s, сбрасывается на успешной
//     сессии и на успешной переавторизации.
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

// moscowLoc — биржа живёт по Москве, а сервер может быть в любой зоне.
// Если в системе нет базы tzdata — откатываемся на фиксированный UTC+3
// (у Москвы перевода часов нет с 2014-го, так что это безопасно).
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

// wsReadDeadline — сколько ждать следующего сообщения, прежде чем считать
// соединение мёртвым. Ночью данных нет по определению, поэтому там окно
// широкое: иначе каждую ночь получали бы ложный обрыв и цикл реконнектов.
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

// timeframe — таймфрейм свечей брокера ("M5", "M15", ...). Отдельный тип,
// чтобы ключ map[timeframe][]ticker читался без сопроводительного комментария.
type timeframe string

// ticker — тикер инструмента ("SBER", "GAZP", ...).
type ticker string

// RouteKey — подписка на свечи конкретного тикера в конкретном таймфрейме.
// Один тикер может иметь несколько одновременных подписок с разным
// Timeframe (разные стратегии, разные таймфреймы) — см. ADR 0001 и
// internal/engine/datafeed.
type RouteKey struct {
	Ticker    string
	Timeframe string
}

// tickerRoutes — вспомогательная развёртка routes для рассылки котировок:
// котировки (Quotes) не привязаны к таймфрейму свечей, поэтому тик должен
// доходить до всех маршрутов тикера независимо от того, на какой Timeframe
// подписан конкретный маршрут.
func tickerRoutes(routes map[RouteKey][]WorkerRoutes) map[ticker][]WorkerRoutes {
	out := make(map[ticker][]WorkerRoutes)
	for key, wr := range routes {
		tk := ticker(key.Ticker)
		out[tk] = append(out[tk], wr...)
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

// SubscribeMarketDataFanOut подписывается на свечи и котировки для набора
// маршрутов, каждый ключ которых — пара (тикер, таймфрейм). Один тикер может
// одновременно иметь несколько маршрутов с разными таймфреймами (разные
// стратегии) — все они уходят на одно WebSocket-соединение отдельными
// subscribe-сообщениями с нужным TimeFrame.
func (c *BCSClient) SubscribeMarketDataFanOut(ctx context.Context, routes map[RouteKey][]WorkerRoutes) error {
	if len(routes) == 0 {
		return fmt.Errorf("список маршрутов пуст")
	}

	// Внешний цикл: держим поток живым, пока не отменят ctx. Каждая итерация —
	// одна WebSocket-сессия; вернулась с ошибкой — ждём backoff и коннектимся
	// заново. Выход только через ctx (отмена сверху).
	backoff := time.Second
	const maxBackoff = 60 * time.Second

	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		err := c.runMarketDataSession(ctx, routes)
		if err != nil {
			// Отмену не считаем сбоем — это штатное завершение.
			if ctx.Err() != nil {
				return ctx.Err()
			}

			// Протухший токен лечится переавторизацией, а не ожиданием:
			// удалось — сразу переподключаемся без паузы и со сброшенным
			// backoff; не удалось — проваливаемся в общий backoff ниже.
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
			// Сессия завершилась без ошибки (обрыв без явного сбоя) —
			// переподключаемся тут же, с чистого листа.
			backoff = time.Second
		}

		// Пауза перед новой попыткой; растёт вдвое за каждый подряд идущий
		// сбой, до потолка. Успешная сессия/переавторизация сбрасывает её.
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

// runMarketDataSession — одна попытка: живёт от установки соединения до
// первой неустранимой ошибки или отмены ctx. Повторами и переавторизацией
// заведует SubscribeMarketDataFanOut, здесь про это думать не надо.
//
// Порядок:
//  1. подключиться (токен уже должен быть получен вызывающей стороной);
//  2. разослать подписки — по свечам отдельно на каждый таймфрейм, по
//     котировкам одним списком всех тикеров;
//  3. поднять keepalive (свой ping, иначе сервер молча отвалится);
//  4. крутить цикл чтения, раздавая каждое сообщение в каналы воркеров,
//     пока соединение живо.
func (c *BCSClient) runMarketDataSession(ctx context.Context, routes map[RouteKey][]WorkerRoutes) error {
	// --- 1. Подключение ---
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

	// --- 2. Подписки ---
	// Один и тот же тикер может встречаться в routes с разными таймфреймами
	// (разные стратегии). Свечи требуют один TimeFrame на subscribe-сообщение,
	// поэтому режем маршруты на группы по таймфрейму. Котировкам таймфрейм не
	// нужен вообще — их собираем в один плоский список уникальных тикеров.
	tickersByTimeframe := make(map[timeframe][]ticker)
	allTickersSet := make(map[ticker]struct{})
	for key := range routes {
		tf := timeframe(key.Timeframe)
		if tf == "" {
			tf = CandleTimeFrame
		}
		tk := ticker(key.Ticker)
		tickersByTimeframe[tf] = append(tickersByTimeframe[tf], tk)
		allTickersSet[tk] = struct{}{}
	}

	// По одному subscribe-сообщению на таймфрейм.
	for tf, tickers := range tickersByTimeframe {
		instruments := make([]wsInstrument, len(tickers))
		for i, tk := range tickers {
			instruments[i] = wsInstrument{ClassCode: classCode, Ticker: string(tk)}
		}
		candleSub := wsSubscribeRequest{
			SubscribeType: 0,
			DataType:      wsDataTypeCandles,
			TimeFrame:     string(tf),
			Instruments:   instruments,
		}
		if err := conn.WriteJSON(candleSub); err != nil {
			return fmt.Errorf("ошибка подписки на свечи (%s): %w", tf, err)
		}
		logx.WS("подписка %d инструмент(ов) %v — свечи %s", len(tickers), tickers, tf)
	}

	// Котировки — одним сообщением на всех.
	allTickers := make([]wsInstrument, 0, len(allTickersSet))
	for tk := range allTickersSet {
		allTickers = append(allTickers, wsInstrument{ClassCode: classCode, Ticker: string(tk)})
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

	// Развёртка маршрутов для котировок: тик прилетает без таймфрейма, поэтому
	// его надо отдать всем подписчикам тикера, какой бы таймфрейм они ни ждали.
	tickRoutes := tickerRoutes(routes)

	// --- 3. Keepalive ---
	// Сервер сам ping не шлёт и молча закрывает тихое соединение. Держим его
	// живым своим ping'ом раз в 30с; на каждый pong отодвигаем read deadline.
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(wsReadDeadline(time.Now())))
	})

	pingTicker := time.NewTicker(30 * time.Second)
	defer pingTicker.Stop()

	// Пингер в фоне. Живёт ровно столько же, сколько сессия: выходит по отмене
	// ctx или по первой ошибке записи (значит, соединение уже мертво и цикл
	// чтения вот-вот вернёт ошибку сам).
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

	// --- 4. Цикл чтения ---
	// Читаем сообщения по одному и раздаём в каналы воркеров, пока соединение
	// живо. Любая ошибка чтения (в т.ч. просроченный read deadline) выходит
	// наверх — там решат, переподключаться или нет.
	for {
		// Неблокирующая проверка отмены: ReadMessage ниже всё равно прервётся
		// по дедлайну, но так выходим сразу, не дожидаясь следующего сообщения.
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Дедлайн пересчитываем каждую итерацию — граница дня/ночи могла
		// сместиться, пока ждали прошлое сообщение.
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

// isUnauthorizedWSError — отличаем "протух токен" от прочих обрывов.
// Структурного признака нет: gorilla/websocket отдаёт только текст ошибки
// рукопожатия, поэтому ищем "HTTP 401" подстрокой. Хрупко, но альтернативы нет.
func isUnauthorizedWSError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "HTTP 401")
}

// dispatchMarketMessage маршрутизирует входящее сообщение. Свечи (CandleStick)
// маршрутизируются по (Ticker, TimeFrame) — так несколько подписок на один
// тикер с разным таймфреймом не путают друг друга. Котировки (Quotes) не
// несут TimeFrame и рассылаются во все маршруты тикера через tickRoutes.
func (c *BCSClient) dispatchMarketMessage(ctx context.Context, raw []byte, routes map[RouteKey][]WorkerRoutes, tickRoutes map[ticker][]WorkerRoutes) error {
	// Сначала читаем только конверт — тип, тикер, таймфрейм, ошибки, — чтобы
	// понять, куда сообщение адресовать. Полезную нагрузку разберёт уже
	// конкретный dispatchCandle/dispatchQuote (да, это второй проход по JSON,
	// но сообщения мелкие, а код так чище).
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

	// Ошибка от сервера (напр. отлуп подписки) — логируем и живём дальше,
	// сессию из-за этого не рвём.
	if len(header.Errors) > 0 {
		logx.WS("ошибка от сервера [%s]: %s (%s)",
			header.Ticker, header.Errors[0].Message, header.Errors[0].Code)
		return nil
	}

	switch header.ResponseType {
	case "CandleStick":
		tf := header.TimeFrame
		if tf == "" {
			tf = CandleTimeFrame
		}
		// Нет маршрута под эту (тикер, таймфрейм) — пришло то, на что мы не
		// подписывались; молча пропускаем.
		routeList, ok := routes[RouteKey{Ticker: header.Ticker, Timeframe: tf}]
		if !ok {
			return nil
		}
		for _, route := range routeList {
			if err := c.dispatchCandle(ctx, raw, route); err != nil {
				return err
			}
		}
	case "Quotes":
		// Котировка идёт всем подписчикам тикера — таймфрейм здесь ни при чём.
		routeList, ok := tickRoutes[ticker(header.Ticker)]
		if !ok {
			return nil
		}
		for _, route := range routeList {
			if err := c.dispatchQuote(ctx, raw, route); err != nil {
				return err
			}
		}
		// Незнакомый responseType — просто игнорируем (default нет намеренно).
	}

	return nil
}

// dispatchCandle разбирает свечное сообщение и кладёт его в канал одного
// маршрута. Битое сообщение или кривой dateTime — пропускаем, сессию не рвём:
// один плохой бар не повод ронять весь поток.
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

	// Отправка блокирующая: если воркер не разгребает свой канал, тормозится
	// весь цикл чтения. Осознанный компромисс — так мы не теряем свечи молча.
	// Разблокирует только приём воркером либо отмена ctx.
	select {
	case route.CandleChan <- candle:
	case <-ctx.Done():
		return ctx.Err()
	}
	return nil
}

// dispatchQuote разбирает котировку и кладёт тик в канал одного маршрута.
// В отличие от свечи, у котировки время не критично: не распарсилось —
// ставим now, тик всё равно нужен для внутрибарового SL/TP.
func (c *BCSClient) dispatchQuote(ctx context.Context, raw []byte, route WorkerRoutes) error {
	if route.TickChan == nil {
		return nil
	}

	var msg quoteWSMessage
	if err := json.Unmarshal(raw, &msg); err != nil {
		return nil
	}

	// last <= 0 — не сделка, а заглушка "данных нет"; такой тик не отдаём.
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

	// Блокирующая отправка — как и для свечей, см. dispatchCandle.
	select {
	case route.TickChan <- tick:
	case <-ctx.Done():
		return ctx.Err()
	}
	return nil
}

// parseWSDateTime — время в сообщениях приходит то с долями секунды, то без;
// пробуем оба формата и приводим к UTC.
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

// formatWSDialError — вытаскиваем в текст ошибки HTTP-статус и тело ответа
// рукопожатия. Именно отсюда потом isUnauthorizedWSError достаёт "HTTP 401".
func formatWSDialError(err error, resp *http.Response) error {
	if resp == nil {
		return fmt.Errorf("ошибка подключения WebSocket: %w", err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	return fmt.Errorf("ошибка подключения WebSocket (HTTP %d): %w, ответ: %s",
		resp.StatusCode, err, string(body))
}
