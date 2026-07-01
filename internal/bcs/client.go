package bcs

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"bcs-trading-bot/pkg/interfaces"
	"bcs-trading-bot/pkg/models"
)

var _ interfaces.OrderExecutor = (*BCSClient)(nil)

// Официальные эндпоинты БКС Trade API
const (
	AuthURL        = "https://be.broker.ru/trade-api-keycloak/realms/tradeapi/protocol/openid-connect/token"
	PortfolioURL   = "https://be.broker.ru/trade-api-bff-portfolio/api/v1/portfolio"
	OperationsURL  = "https://be.broker.ru/trade-api-bff-operations/api/v1"
	ClientIDRead   = "trade-api-read"
	ClientIDWrite  = "trade-api-write"
)

type BCSClient struct {
	httpClient      *http.Client
	refreshToken    string
	accessToken     string
	classCode       string
	candleTimeFrame string
	clientID        string
}

func NewBCSClient(refreshToken string) *BCSClient {
	return &BCSClient{
		httpClient:      &http.Client{Timeout: 15 * time.Second},
		refreshToken:    refreshToken,
		classCode:       DefaultClassCode,
		candleTimeFrame: CandleTimeFrame,
		clientID:        ClientIDRead,
	}
}

// SetClassCode задаёт код класса инструмента (например, TQBR или SPBFUT).
func (c *BCSClient) SetClassCode(code string) {
	c.classCode = code
}

// SetCandleTimeFrame задаёт таймфрейм свечей для WebSocket-подписки (M1, M5, ...).
func (c *BCSClient) SetCandleTimeFrame(tf string) {
	c.candleTimeFrame = tf
}

// SetWriteMode переключает OAuth client_id на trade-api-write для реальной торговли.
func (c *BCSClient) SetWriteMode() {
	c.clientID = ClientIDWrite
}

// AccessToken возвращает текущий OAuth2 access token.
func (c *BCSClient) AccessToken() string {
	return c.accessToken
}

func (c *BCSClient) Connect(ctx context.Context) error {
	formData := fmt.Sprintf(
		"grant_type=refresh_token&refresh_token=%s&client_id=%s",
		c.refreshToken, c.clientID,
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, AuthURL, bytes.NewBufferString(formData))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("ошибка сети при авторизации: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("брокер отклонил авторизацию, статус: %d, ответ: %s", resp.StatusCode, string(body))
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return err
	}

	token, ok := result["access_token"].(string)
	if !ok {
		return fmt.Errorf("access_token не найден в ответе Keycloak")
	}
	c.accessToken = token
	return nil
}

func (c *BCSClient) GetPortfolio(ctx context.Context) (string, error) {
	if c.accessToken == "" {
		return "", fmt.Errorf("клиент не авторизован")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, PortfolioURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.accessToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("ошибка получения портфеля, статус: %d", resp.StatusCode)
	}

	var rawJSON json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&rawJSON); err != nil {
		return "", err
	}
	return string(rawJSON), nil
}

type portfolioResponse struct {
	Summary struct {
		Cash       float64 `json:"cash"`
		TotalValue float64 `json:"totalValue"`
	} `json:"summary"`
}

type createOrderRequest struct {
	ClientOrderID string   `json:"clientOrderId"`
	Side          string   `json:"side"`
	OrderType     string   `json:"orderType"`
	OrderQuantity int      `json:"orderQuantity"`
	Ticker        string   `json:"ticker"`
	ClassCode     string   `json:"classCode"`
	Price         *float64 `json:"price,omitempty"`
}

// ExecuteOrder отправляет ордер в BCS Trade API (лимитный или рыночный).
func (c *BCSClient) ExecuteOrder(ctx context.Context, order models.Order) error {
	if c.accessToken == "" {
		return fmt.Errorf("клиент не авторизован")
	}

	side := "1" // BUY
	if order.Direction == "SELL" {
		side = "2"
	}

	classCode := c.classCode
	if classCode == "" {
		classCode = DefaultClassCode
	}

	orderType := "2" // limit
	var pricePtr *float64
	if order.OrderType == models.OrderTypeMarket {
		orderType = "1"
	} else {
		pricePtr = &order.Price
	}

	body, err := json.Marshal(createOrderRequest{
		ClientOrderID: newClientOrderID(),
		Side:          side,
		OrderType:     orderType,
		OrderQuantity: order.Quantity,
		Ticker:        order.Ticker,
		ClassCode:     classCode,
		Price:         pricePtr,
	})
	if err != nil {
		return fmt.Errorf("ошибка сериализации ордера: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, OperationsURL+"/orders", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.accessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("ошибка отправки ордера: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("ошибка чтения ответа: %w", err)
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("брокер отклонил ордер, статус: %d, ответ: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// GetBalance возвращает свободные средства из портфеля.
func (c *BCSClient) GetBalance(ctx context.Context) (float64, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	raw, err := c.GetPortfolio(ctx)
	if err != nil {
		return 0, err
	}

	var portfolio portfolioResponse
	if err := json.Unmarshal([]byte(raw), &portfolio); err != nil {
		return 0, fmt.Errorf("ошибка разбора портфеля: %w", err)
	}

	if portfolio.Summary.Cash > 0 {
		return portfolio.Summary.Cash, nil
	}
	return portfolio.Summary.TotalValue, nil
}

func newClientOrderID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
