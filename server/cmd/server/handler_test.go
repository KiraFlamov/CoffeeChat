package main

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/coder/websocket"
	"google.golang.org/protobuf/proto"

	coffeechatv1 "git.sourcecraft.dev/drflamov/coffeechat/server/gen/coffeechat/v1"
	"git.sourcecraft.dev/drflamov/coffeechat/server/gen/coffeechat/v1/coffeechatv1connect"
	"git.sourcecraft.dev/drflamov/coffeechat/server/internal/core"
	"git.sourcecraft.dev/drflamov/coffeechat/server/internal/port"
	"git.sourcecraft.dev/drflamov/coffeechat/server/internal/transport/hub"
)

const testOrigin = "http://localhost:5173"

// fakeStore — подделка хранилища в памяти.
type fakeStore struct {
	mu     sync.Mutex
	conns  map[string]port.Connection
	putErr error
	getErr error
}

func newFakeStore() *fakeStore {
	return &fakeStore{conns: map[string]port.Connection{}}
}

func (s *fakeStore) Ping(context.Context) error { return nil }

func (s *fakeStore) PutConnection(_ context.Context, c port.Connection) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.putErr != nil {
		return s.putErr
	}
	s.conns[c.ID] = c
	return nil
}

func (s *fakeStore) GetConnection(_ context.Context, id string) (port.Connection, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.getErr != nil {
		return port.Connection{}, s.getErr
	}
	c, ok := s.conns[id]
	if !ok {
		return port.Connection{}, port.ErrNotFound
	}
	return c, nil
}

func (s *fakeStore) DeleteConnection(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.conns, id)
	return nil
}

func (s *fakeStore) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.conns)
}

func (s *fakeStore) setGetErr(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.getErr = err
}

// startServer поднимает локальный сервер с подделкой базы и настоящим хабом.
func startServer(t *testing.T, store *fakeStore) *httptest.Server {
	t.Helper()
	origin, err := parseOrigin(testOrigin)
	if err != nil {
		t.Fatalf("parseOrigin: %v", err)
	}
	connections := hub.New()
	system := core.NewSystem(store, connections, "test", time.Now)
	srv := httptest.NewServer(newHandler(system, connections, origin))
	t.Cleanup(srv.Close)
	return srv
}

func testContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func dial(t *testing.T, ctx context.Context, srv *httptest.Server, header http.Header) *websocket.Conn {
	t.Helper()
	conn, resp, err := websocket.Dial(ctx, srv.URL+"/ws", &websocket.DialOptions{HTTPHeader: header})
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.CloseNow() })
	return conn
}

func sendEcho(t *testing.T, ctx context.Context, conn *websocket.Conn, nonce []byte, viaSendAPI bool) {
	t.Helper()
	data, err := proto.Marshal(&coffeechatv1.ClientFrame{
		Payload: &coffeechatv1.ClientFrame_Echo{
			Echo: &coffeechatv1.Echo{Nonce: nonce, ViaSendApi: viaSendAPI},
		},
	})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if err := conn.Write(ctx, websocket.MessageBinary, data); err != nil {
		t.Fatalf("Write: %v", err)
	}
}

// readFrame читает один кадр сервера и проверяет, что он двоичный.
func readFrame(t *testing.T, ctx context.Context, conn *websocket.Conn) *coffeechatv1.ServerFrame {
	t.Helper()
	typ, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if typ != websocket.MessageBinary {
		t.Fatalf("кадр сервера типа %v, ожидался двоичный", typ)
	}
	var frame coffeechatv1.ServerFrame
	if err := proto.Unmarshal(data, &frame); err != nil {
		t.Fatalf("кадр сервера не разбирается: %v", err)
	}
	return &frame
}

func wantEcho(t *testing.T, frame *coffeechatv1.ServerFrame, nonce []byte, viaSendAPI bool) {
	t.Helper()
	echo := frame.GetEcho()
	if echo == nil {
		t.Fatalf("получен кадр %v, ожидался Echo", frame)
	}
	if !bytes.Equal(echo.GetNonce(), nonce) || echo.GetViaSendApi() != viaSendAPI {
		t.Fatalf("Echo{%x, %v}, ожидался Echo{%x, %v}", echo.GetNonce(), echo.GetViaSendApi(), nonce, viaSendAPI)
	}
}

func wantError(t *testing.T, frame *coffeechatv1.ServerFrame, code string) {
	t.Helper()
	if got := frame.GetError().GetCode(); got != code {
		t.Fatalf("получен кадр %v, ожидался Error с кодом %q", frame, code)
	}
}

func TestPingOverHTTP(t *testing.T) {
	srv := startServer(t, newFakeStore())
	client := coffeechatv1connect.NewSystemServiceClient(srv.Client(), srv.URL)
	nonce := []byte("ping-nonce")

	res, err := client.Ping(testContext(t), connect.NewRequest(&coffeechatv1.PingRequest{Nonce: nonce}))
	if err != nil {
		t.Fatalf("Ping: %v", err)
	}
	if !bytes.Equal(res.Msg.GetNonce(), nonce) || !res.Msg.GetStoreOk() {
		t.Fatalf("ответ %v", res.Msg)
	}
	if got := res.Header().Get("Access-Control-Allow-Origin"); got != testOrigin {
		t.Errorf("Access-Control-Allow-Origin = %q, ожидался %q", got, testOrigin)
	}
}

func TestEchoBothWays(t *testing.T) {
	store := newFakeStore()
	srv := startServer(t, store)
	ctx := testContext(t)
	conn := dial(t, ctx, srv, nil)

	// Ответ «телом»: сервер отвечает в том же соединении сам.
	sendEcho(t, ctx, conn, []byte("body"), false)
	wantEcho(t, readFrame(t, ctx, conn), []byte("body"), false)

	// Ответ через Transport.Send: локально это хаб.
	sendEcho(t, ctx, conn, []byte("send-api"), true)
	wantEcho(t, readFrame(t, ctx, conn), []byte("send-api"), true)

	if store.count() != 1 {
		t.Fatalf("в базе %d соединений, ожидалось 1", store.count())
	}
}

func TestDisconnectRemovesConnection(t *testing.T) {
	store := newFakeStore()
	srv := startServer(t, store)
	ctx := testContext(t)
	conn := dial(t, ctx, srv, nil)

	// Обмен кадрами гарантирует, что сервер уже записал соединение.
	sendEcho(t, ctx, conn, []byte("x"), false)
	readFrame(t, ctx, conn)
	if err := conn.Close(websocket.StatusNormalClosure, ""); err != nil {
		t.Fatalf("Close: %v", err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for store.count() != 0 {
		if time.Now().After(deadline) {
			t.Fatalf("запись о соединении не удалена после закрытия")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestBadFramesKeepConnectionOpen(t *testing.T) {
	srv := startServer(t, newFakeStore())
	ctx := testContext(t)
	conn := dial(t, ctx, srv, nil)

	// Текстовый кадр отвергает точка входа.
	if err := conn.Write(ctx, websocket.MessageText, []byte("hello")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	wantError(t, readFrame(t, ctx, conn), "bad_frame")

	// Двоичный мусор отвергает ядро.
	if err := conn.Write(ctx, websocket.MessageBinary, []byte{0xff, 0xff, 0xff}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	wantError(t, readFrame(t, ctx, conn), "bad_frame")

	// nonce длиннее 32 байт.
	sendEcho(t, ctx, conn, make([]byte, core.MaxNonceLen+1), false)
	wantError(t, readFrame(t, ctx, conn), "too_large")

	// Соединение после ошибок живо.
	sendEcho(t, ctx, conn, []byte("alive"), false)
	wantEcho(t, readFrame(t, ctx, conn), []byte("alive"), false)
}

func TestStoreFailureOnMessageIsInternalError(t *testing.T) {
	store := newFakeStore()
	srv := startServer(t, store)
	ctx := testContext(t)
	conn := dial(t, ctx, srv, nil)

	store.setGetErr(errors.New("db down"))
	sendEcho(t, ctx, conn, []byte("x"), true)
	frame := readFrame(t, ctx, conn)
	wantError(t, frame, "internal")
	if strings.Contains(frame.GetError().GetMessage(), "db down") {
		t.Fatalf("текст внутренней ошибки ушёл клиенту: %q", frame.GetError().GetMessage())
	}
}

func TestTooLargeMessageClosesConnection(t *testing.T) {
	srv := startServer(t, newFakeStore())
	ctx := testContext(t)
	conn := dial(t, ctx, srv, nil)

	if err := conn.Write(ctx, websocket.MessageBinary, make([]byte, maxMessageBytes+1)); err != nil {
		t.Fatalf("Write: %v", err)
	}
	_, _, err := conn.Read(ctx)
	if got := websocket.CloseStatus(err); got != websocket.StatusMessageTooBig {
		t.Fatalf("код закрытия %d (%v), ожидался 1009", got, err)
	}
}

func TestStoreFailureOnConnectRejectsHandshake(t *testing.T) {
	store := newFakeStore()
	store.putErr = errors.New("db down")
	srv := startServer(t, store)

	conn, resp, err := websocket.Dial(testContext(t), srv.URL+"/ws", nil)
	if err == nil {
		_ = conn.CloseNow()
		t.Fatal("соединение открылось при недоступной базе")
	}
	if resp == nil {
		t.Fatalf("нет HTTP-ответа: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("код %d, ожидался 503", resp.StatusCode)
	}
}

func TestWebSocketOrigin(t *testing.T) {
	store := newFakeStore()
	srv := startServer(t, store)
	ctx := testContext(t)

	// Наш фронтенд проходит.
	conn := dial(t, ctx, srv, http.Header{"Origin": {testOrigin}})
	sendEcho(t, ctx, conn, []byte("x"), false)
	wantEcho(t, readFrame(t, ctx, conn), []byte("x"), false)

	// Чужой сайт — нет.
	bad, resp, err := websocket.Dial(ctx, srv.URL+"/ws", &websocket.DialOptions{
		HTTPHeader: http.Header{"Origin": {"https://evil.example"}},
	})
	if err == nil {
		_ = bad.CloseNow()
		t.Fatal("соединение с чужим Origin открылось")
	}
	if resp == nil {
		t.Fatalf("нет HTTP-ответа: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("код %d, ожидался 403", resp.StatusCode)
	}

	// Отвергнутое соединение не оставляет записи в базе.
	deadline := time.Now().Add(3 * time.Second)
	for store.count() != 1 {
		if time.Now().After(deadline) {
			t.Fatalf("в базе %d соединений, ожидалось 1", store.count())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestCORSPreflight(t *testing.T) {
	srv := startServer(t, newFakeStore())
	req, err := http.NewRequestWithContext(testContext(t), http.MethodOptions,
		srv.URL+coffeechatv1connect.SystemServicePingProcedure, nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Origin", testOrigin)
	req.Header.Set("Access-Control-Request-Method", http.MethodPost)
	req.Header.Set("Access-Control-Request-Headers", "content-type,connect-protocol-version")

	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("код %d, ожидался 204", resp.StatusCode)
	}
	want := map[string]string{
		"Access-Control-Allow-Origin":  testOrigin,
		"Access-Control-Allow-Methods": http.MethodPost,
		"Access-Control-Allow-Headers": "Content-Type, Connect-Protocol-Version, Connect-Timeout-Ms",
		"Access-Control-Max-Age":       corsMaxAge,
	}
	for name, value := range want {
		if got := resp.Header.Get(name); got != value {
			t.Errorf("%s = %q, ожидалось %q", name, got, value)
		}
	}
}

// Чужому origin сервер отвечает нашим origin: браузер такой ответ чужому сайту не отдаст.
func TestCORSForeignOrigin(t *testing.T) {
	srv := startServer(t, newFakeStore())
	body, err := proto.Marshal(&coffeechatv1.PingRequest{})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	req, err := http.NewRequestWithContext(testContext(t), http.MethodPost,
		srv.URL+coffeechatv1connect.SystemServicePingProcedure, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Origin", "https://evil.example")
	req.Header.Set("Content-Type", "application/proto")

	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != testOrigin {
		t.Fatalf("Access-Control-Allow-Origin = %q, ожидался %q", got, testOrigin)
	}
}

func TestParseOrigin(t *testing.T) {
	for _, raw := range []string{"", "localhost:5173", "ftp://localhost", "http://localhost:5173/app", "*"} {
		if _, err := parseOrigin(raw); err == nil {
			t.Errorf("parseOrigin(%q) без ошибки", raw)
		}
	}
	u, err := parseOrigin(testOrigin)
	if err != nil {
		t.Fatalf("parseOrigin: %v", err)
	}
	if (&url.URL{Scheme: u.Scheme, Host: u.Host}).String() != testOrigin {
		t.Fatalf("origin %q", u.String())
	}
}
