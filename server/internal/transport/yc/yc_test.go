package yc_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"git.sourcecraft.dev/drflamov/coffeechat/server/internal/port"
	"git.sourcecraft.dev/drflamov/coffeechat/server/internal/transport/yc"
)

// cloud — подставное облако: сервис метаданных и Send API в одном HTTP-сервере.
type cloud struct {
	t   *testing.T
	srv *httptest.Server

	mu          sync.Mutex
	tokenCalls  int
	tokenStatus int
	expiresIn   int64
	sendStatus  []int // коды ответов Send API по порядку; дальше — 200
	sends       []sendCall
}

type sendCall struct {
	path  string
	auth  string
	ctype string
	body  []byte
}

func newCloud(t *testing.T) *cloud {
	t.Helper()
	c := &cloud{t: t, tokenStatus: http.StatusOK, expiresIn: 3600}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /token", c.handleToken)
	mux.HandleFunc("POST /apigateways/websocket/v1/connections/", c.handleSend)
	c.srv = httptest.NewServer(mux)
	t.Cleanup(c.srv.Close)
	return c
}

func (c *cloud) handleToken(w http.ResponseWriter, r *http.Request) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if r.Header.Get("Metadata-Flavor") != "Google" {
		c.t.Errorf("запрос токена без заголовка Metadata-Flavor")
	}
	c.tokenCalls++
	if c.tokenStatus != http.StatusOK {
		w.WriteHeader(c.tokenStatus)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"access_token": "secret-token-" + strconv.Itoa(c.tokenCalls),
		"expires_in":   c.expiresIn,
		"token_type":   "Bearer",
	})
}

func (c *cloud) handleSend(w http.ResponseWriter, r *http.Request) {
	c.mu.Lock()
	defer c.mu.Unlock()
	body, _ := io.ReadAll(r.Body)
	c.sends = append(c.sends, sendCall{
		path:  r.URL.EscapedPath(),
		auth:  r.Header.Get("Authorization"),
		ctype: r.Header.Get("Content-Type"),
		body:  body,
	})
	status := http.StatusOK
	if len(c.sendStatus) > 0 {
		status, c.sendStatus = c.sendStatus[0], c.sendStatus[1:]
	}
	w.WriteHeader(status)
	if status != http.StatusOK {
		_, _ = w.Write([]byte(`{"message":"api says no"}`))
	}
}

func (c *cloud) transport(opts ...yc.Option) *yc.Transport {
	base := []yc.Option{
		yc.WithSendURL(c.srv.URL),
		yc.WithTokenURL(c.srv.URL + "/token"),
		yc.WithHTTPClient(c.srv.Client()),
	}
	return yc.New(append(base, opts...)...)
}

func TestSend(t *testing.T) {
	c := newCloud(t)
	tr := c.transport()
	data := []byte{0x00, 0xff, 0x10, 0x20}

	if err := tr.Send(context.Background(), "conn-1", data); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if len(c.sends) != 1 {
		t.Fatalf("запросов к Send API: %d, ожидался 1", len(c.sends))
	}
	call := c.sends[0]
	if call.path != "/apigateways/websocket/v1/connections/conn-1:send" {
		t.Errorf("путь %q", call.path)
	}
	if call.auth != "Bearer secret-token-1" {
		t.Errorf("Authorization %q", call.auth)
	}
	if call.ctype != "application/json" {
		t.Errorf("Content-Type %q", call.ctype)
	}
	var body struct {
		Data string `json:"data"`
		Type string `json:"type"`
	}
	if err := json.Unmarshal(call.body, &body); err != nil {
		t.Fatalf("тело запроса не JSON: %v", err)
	}
	if body.Type != "BINARY" || body.Data != base64.StdEncoding.EncodeToString(data) {
		t.Errorf("тело запроса %+v", body)
	}
}

func TestSendEscapesConnectionID(t *testing.T) {
	c := newCloud(t)
	if err := c.transport().Send(context.Background(), "a/b?c", []byte{1}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if got := c.sends[0].path; got != "/apigateways/websocket/v1/connections/a%2Fb%3Fc:send" {
		t.Errorf("путь %q", got)
	}
}

func TestSendClosedConnectionIsGone(t *testing.T) {
	for _, status := range []int{http.StatusNotFound, http.StatusGone} {
		c := newCloud(t)
		c.sendStatus = []int{status}
		if err := c.transport().Send(context.Background(), "conn-1", []byte{1}); !errors.Is(err, port.ErrGone) {
			t.Errorf("код %d: ошибка %v, ожидалась ErrGone", status, err)
		}
	}
}

func TestSendServerErrorIsNotGone(t *testing.T) {
	c := newCloud(t)
	c.sendStatus = []int{http.StatusInternalServerError}

	err := c.transport().Send(context.Background(), "conn-1", []byte{1})
	if err == nil || errors.Is(err, port.ErrGone) {
		t.Fatalf("ошибка %v, ожидалась обычная ошибка", err)
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("в ошибке нет кода ответа: %v", err)
	}
	if strings.Contains(err.Error(), "secret-token") {
		t.Errorf("токен попал в текст ошибки: %v", err)
	}
}

func TestTokenIsCachedUntilExpiry(t *testing.T) {
	c := newCloud(t)
	now := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	tr := c.transport(yc.WithClock(func() time.Time { return now }))
	ctx := context.Background()

	for range 3 {
		if err := tr.Send(ctx, "conn-1", []byte{1}); err != nil {
			t.Fatalf("Send: %v", err)
		}
	}
	if c.tokenCalls != 1 {
		t.Fatalf("запросов токена: %d, ожидался 1", c.tokenCalls)
	}

	// За минуту до конца срока токен уже считается устаревшим.
	now = now.Add(time.Hour - 30*time.Second)
	if err := tr.Send(ctx, "conn-1", []byte{1}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if c.tokenCalls != 2 {
		t.Fatalf("запросов токена: %d, ожидалось 2", c.tokenCalls)
	}
	if got := c.sends[3].auth; got != "Bearer secret-token-2" {
		t.Errorf("Authorization %q, ожидался новый токен", got)
	}
}

func TestUnauthorizedRefreshesTokenOnce(t *testing.T) {
	c := newCloud(t)
	c.sendStatus = []int{http.StatusUnauthorized}
	if err := c.transport().Send(context.Background(), "conn-1", []byte{1}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if c.tokenCalls != 2 || len(c.sends) != 2 {
		t.Fatalf("запросов токена %d, отправок %d; ожидалось 2 и 2", c.tokenCalls, len(c.sends))
	}

	// Второй отказ подряд — ошибка, без бесконечных повторов.
	c.sendStatus = []int{http.StatusUnauthorized, http.StatusUnauthorized}
	err := c.transport().Send(context.Background(), "conn-1", []byte{1})
	if err == nil || errors.Is(err, port.ErrGone) {
		t.Fatalf("ошибка %v, ожидалась обычная ошибка", err)
	}
}

func TestMetadataFailure(t *testing.T) {
	c := newCloud(t)
	c.tokenStatus = http.StatusInternalServerError

	err := c.transport().Send(context.Background(), "conn-1", []byte{1})
	if err == nil || errors.Is(err, port.ErrGone) {
		t.Fatalf("ошибка %v, ожидалась обычная ошибка", err)
	}
	if len(c.sends) != 0 {
		t.Errorf("Send API вызван без токена")
	}
}

func TestConcurrentSend(t *testing.T) {
	c := newCloud(t)
	tr := c.transport()
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := tr.Send(context.Background(), "conn-1", []byte{1}); err != nil {
				t.Errorf("Send: %v", err)
			}
		}()
	}
	wg.Wait()
	if c.tokenCalls != 1 {
		t.Errorf("запросов токена: %d, ожидался 1", c.tokenCalls)
	}
}
