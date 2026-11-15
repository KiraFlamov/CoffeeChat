// Package yc — адаптер порта Transport для облака: отправляет кадры в соединение
// WebSocket через Send API шлюза Yandex API Gateway. Локально вместо него transport/hub.
package yc

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	"git.sourcecraft.dev/drflamov/coffeechat/server/internal/port"
)

const (
	// DefaultSendURL — адрес Send API шлюза (ADR-002).
	DefaultSendURL = "https://apigateway-connections.api.cloud.yandex.net"
	// DefaultTokenURL — сервис метаданных: выдаёт IAM-токен сервисного аккаунта контейнера.
	DefaultTokenURL = "http://169.254.169.254/computeMetadata/v1/instance/service-accounts/default/token" //nolint:gosec // это адрес, а не секрет

	requestTimeout = 5 * time.Second
	// tokenMargin — за сколько до конца срока токен считается устаревшим.
	tokenMargin = time.Minute
	// maxErrorBody — сколько байт ответа с ошибкой попадает в текст ошибки.
	maxErrorBody = 300
	// maxTokenBody — предел ответа сервиса метаданных.
	maxTokenBody = 16 << 10
)

// Transport отправляет кадры через Send API. Методы можно вызывать одновременно.
// В памяти хранится только IAM-токен до конца его срока; состояния соединений здесь нет.
type Transport struct {
	client   *http.Client
	sendURL  string
	tokenURL string
	now      func() time.Time

	mu      sync.Mutex
	token   string
	expires time.Time
}

var _ port.Transport = (*Transport)(nil)

// Option меняет настройки адаптера. Нужны тестам: в бою хватает значений по умолчанию.
type Option func(*Transport)

// WithSendURL задаёт адрес Send API.
func WithSendURL(u string) Option { return func(t *Transport) { t.sendURL = u } }

// WithTokenURL задаёт адрес сервиса метаданных.
func WithTokenURL(u string) Option { return func(t *Transport) { t.tokenURL = u } }

// WithHTTPClient задаёт HTTP-клиент.
func WithHTTPClient(c *http.Client) Option { return func(t *Transport) { t.client = c } }

// WithClock задаёт источник времени.
func WithClock(now func() time.Time) Option { return func(t *Transport) { t.now = now } }

// New создаёт адаптер.
func New(opts ...Option) *Transport {
	t := &Transport{
		client:   &http.Client{Timeout: requestTimeout},
		sendURL:  DefaultSendURL,
		tokenURL: DefaultTokenURL,
		now:      time.Now,
	}
	for _, opt := range opts {
		opt(t)
	}
	return t
}

// Send отправляет двоичный кадр в соединение. Закрытое соединение — port.ErrGone.
func (t *Transport) Send(ctx context.Context, connectionID string, data []byte) error {
	body, err := json.Marshal(sendRequest{
		Data: base64.StdEncoding.EncodeToString(data),
		Type: "BINARY",
	})
	if err != nil {
		return fmt.Errorf("send api: marshal: %w", err)
	}
	endpoint := t.sendURL + "/apigateways/websocket/v1/connections/" + url.PathEscape(connectionID) + ":send"

	status, detail, err := t.post(ctx, endpoint, body)
	if err != nil {
		return err
	}
	if status == http.StatusUnauthorized {
		// Токен мог быть отозван раньше срока: берём новый и пробуем ещё один раз.
		t.dropToken()
		if status, detail, err = t.post(ctx, endpoint, body); err != nil {
			return err
		}
	}
	switch status {
	case http.StatusOK:
		return nil
	case http.StatusNotFound, http.StatusGone:
		return port.ErrGone
	default:
		return fmt.Errorf("send api: status %d: %s", status, detail)
	}
}

type sendRequest struct {
	Data string `json:"data"`
	Type string `json:"type"`
}

// post выполняет один запрос к Send API. Возвращает код ответа и начало тела ответа.
func (t *Transport) post(ctx context.Context, endpoint string, body []byte) (int, string, error) {
	token, err := t.iamToken(ctx)
	if err != nil {
		return 0, "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return 0, "", fmt.Errorf("send api: new request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := t.client.Do(req)
	if err != nil {
		return 0, "", fmt.Errorf("send api: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	detail, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
	return resp.StatusCode, string(detail), nil
}

// iamToken возвращает действующий IAM-токен; новый запрашивает, когда старый истекает.
func (t *Transport) iamToken(ctx context.Context) (string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.token != "" && t.now().Before(t.expires) {
		return t.token, nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, t.tokenURL, nil)
	if err != nil {
		return "", fmt.Errorf("metadata token: new request: %w", err)
	}
	req.Header.Set("Metadata-Flavor", "Google")
	resp, err := t.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("metadata token: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("metadata token: status %d", resp.StatusCode)
	}
	var parsed struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"` // секунды
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxTokenBody)).Decode(&parsed); err != nil {
		return "", fmt.Errorf("metadata token: decode: %w", err)
	}
	if parsed.AccessToken == "" {
		return "", fmt.Errorf("metadata token: empty token in response")
	}
	t.token = parsed.AccessToken
	t.expires = t.now().Add(time.Duration(parsed.ExpiresIn)*time.Second - tokenMargin)
	return t.token, nil
}

func (t *Transport) dropToken() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.token = ""
}
