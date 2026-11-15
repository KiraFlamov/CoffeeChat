package main

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/proto"

	coffeechatv1 "git.sourcecraft.dev/drflamov/coffeechat/server/gen/coffeechat/v1"
	"git.sourcecraft.dev/drflamov/coffeechat/server/gen/coffeechat/v1/coffeechatv1connect"
	"git.sourcecraft.dev/drflamov/coffeechat/server/internal/core"
	"git.sourcecraft.dev/drflamov/coffeechat/server/internal/port"
)

// fakeStore — подделка хранилища в памяти.
type fakeStore struct {
	mu     sync.Mutex
	conns  map[string]port.Connection
	putErr error
	getErr error
	delErr error
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
	if s.delErr != nil {
		return s.delErr
	}
	delete(s.conns, id)
	return nil
}

func (s *fakeStore) has(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.conns[id]
	return ok
}

// fakeTransport запоминает отправленное через Send API.
type fakeTransport struct {
	mu   sync.Mutex
	sent map[string][][]byte
	err  error
}

func (f *fakeTransport) Send(_ context.Context, id string, data []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	if f.sent == nil {
		f.sent = map[string][][]byte{}
	}
	f.sent[id] = append(f.sent[id], data)
	return nil
}

type fixture struct {
	store     *fakeStore
	transport *fakeTransport
	handler   http.Handler
}

func newFixture() *fixture {
	f := &fixture{store: newFakeStore(), transport: &fakeTransport{}}
	f.handler = newHandler(core.NewSystem(f.store, f.transport, "test", time.Now))
	return f
}

// event отправляет обработчику событие шлюза и возвращает ответ.
func (f *fixture) event(t *testing.T, eventType, connID, contentType string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/ws", bytes.NewReader(body))
	if eventType != "" {
		req.Header.Set(headerEventType, eventType)
	}
	if connID != "" {
		req.Header.Set(headerConnectionID, connID)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, req)
	return rec
}

func echoFrame(t *testing.T, nonce []byte, viaSendAPI bool) []byte {
	t.Helper()
	data, err := proto.Marshal(&coffeechatv1.ClientFrame{
		Payload: &coffeechatv1.ClientFrame_Echo{
			Echo: &coffeechatv1.Echo{Nonce: nonce, ViaSendApi: viaSendAPI},
		},
	})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	return data
}

// readFrame проверяет, что ответ уйдёт клиенту двоичным кадром, и разбирает его.
func readFrame(t *testing.T, rec *httptest.ResponseRecorder) *coffeechatv1.ServerFrame {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("код %d, ожидался 200", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != binaryContentType {
		t.Fatalf("Content-Type %q, ожидался %q", got, binaryContentType)
	}
	var frame coffeechatv1.ServerFrame
	if err := proto.Unmarshal(rec.Body.Bytes(), &frame); err != nil {
		t.Fatalf("ответ не разбирается как ServerFrame: %v", err)
	}
	return &frame
}

func wantEmpty(t *testing.T, rec *httptest.ResponseRecorder, status int) {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("код %d, ожидался %d", rec.Code, status)
	}
	if status == http.StatusOK && rec.Body.Len() != 0 {
		t.Fatalf("тело ответа %d байт, ожидалось пустое", rec.Body.Len())
	}
}

func TestConnectAndDisconnect(t *testing.T) {
	f := newFixture()

	wantEmpty(t, f.event(t, eventConnect, "c1", "", nil), http.StatusOK)
	if !f.store.has("c1") {
		t.Fatal("после CONNECT соединения нет в базе")
	}
	// Повторный CONNECT — не ошибка.
	wantEmpty(t, f.event(t, eventConnect, "c1", "", nil), http.StatusOK)

	wantEmpty(t, f.event(t, eventDisconnect, "c1", "", nil), http.StatusOK)
	if f.store.has("c1") {
		t.Fatal("после DISCONNECT соединение осталось в базе")
	}
	// Повторный DISCONNECT и DISCONNECT без CONNECT — не ошибка.
	wantEmpty(t, f.event(t, eventDisconnect, "c1", "", nil), http.StatusOK)
	wantEmpty(t, f.event(t, eventDisconnect, "never-connected", "", nil), http.StatusOK)
}

func TestConnectWhenStoreIsDown(t *testing.T) {
	f := newFixture()
	f.store.putErr = errors.New("db down")

	rec := f.event(t, eventConnect, "c1", "", nil)
	if rec.Code < 500 {
		t.Fatalf("код %d, ожидался 5xx: шлюз не должен открыть соединение", rec.Code)
	}
}

func TestMessageEchoInBody(t *testing.T) {
	f := newFixture()
	f.event(t, eventConnect, "c1", "", nil)

	rec := f.event(t, eventMessage, "c1", binaryContentType, echoFrame(t, []byte("body"), false))
	echo := readFrame(t, rec).GetEcho()
	if echo == nil || !bytes.Equal(echo.GetNonce(), []byte("body")) || echo.GetViaSendApi() {
		t.Fatalf("ответ %v", echo)
	}
	if len(f.transport.sent) != 0 {
		t.Fatal("Send API вызван для ответа телом")
	}
}

func TestMessageEchoViaSendAPI(t *testing.T) {
	f := newFixture()
	f.event(t, eventConnect, "c1", "", nil)

	rec := f.event(t, eventMessage, "c1", binaryContentType, echoFrame(t, []byte("send"), true))
	wantEmpty(t, rec, http.StatusOK)

	sent := f.transport.sent["c1"]
	if len(sent) != 1 {
		t.Fatalf("кадров через Send API: %d, ожидался 1", len(sent))
	}
	var frame coffeechatv1.ServerFrame
	if err := proto.Unmarshal(sent[0], &frame); err != nil {
		t.Fatalf("кадр не разбирается: %v", err)
	}
	if echo := frame.GetEcho(); echo == nil || !bytes.Equal(echo.GetNonce(), []byte("send")) || !echo.GetViaSendApi() {
		t.Fatalf("кадр %v", &frame)
	}
}

func TestMessageViaSendAPIWhenConnectionIsGone(t *testing.T) {
	f := newFixture()
	f.event(t, eventConnect, "c1", "", nil)
	f.transport.err = port.ErrGone

	rec := f.event(t, eventMessage, "c1", binaryContentType, echoFrame(t, []byte("x"), true))
	wantEmpty(t, rec, http.StatusOK)
	if f.store.has("c1") {
		t.Fatal("запись о закрытом соединении не удалена")
	}
}

func TestTextFrameIsBadFrame(t *testing.T) {
	f := newFixture()
	for _, contentType := range []string{"application/json", "text/plain; charset=utf-8", ""} {
		rec := f.event(t, eventMessage, "c1", contentType, []byte(`"hello"`))
		if got := readFrame(t, rec).GetError().GetCode(); got != "bad_frame" {
			t.Errorf("Content-Type %q: код %q, ожидался bad_frame", contentType, got)
		}
	}
}

func TestGarbageAndTooLargeNonce(t *testing.T) {
	f := newFixture()

	rec := f.event(t, eventMessage, "c1", binaryContentType, []byte{0xff, 0xff, 0xff})
	if got := readFrame(t, rec).GetError().GetCode(); got != "bad_frame" {
		t.Errorf("мусор: код %q, ожидался bad_frame", got)
	}

	big := echoFrame(t, make([]byte, core.MaxNonceLen+1), false)
	rec = f.event(t, eventMessage, "c1", binaryContentType+"; charset=binary", big)
	if got := readFrame(t, rec).GetError().GetCode(); got != "too_large" {
		t.Errorf("длинный nonce: код %q, ожидался too_large", got)
	}
}

func TestStoreFailureOnMessageIsInternalError(t *testing.T) {
	f := newFixture()
	f.store.getErr = errors.New("db down")

	rec := f.event(t, eventMessage, "c1", binaryContentType, echoFrame(t, []byte("x"), true))
	frame := readFrame(t, rec)
	if got := frame.GetError().GetCode(); got != "internal" {
		t.Fatalf("код %q, ожидался internal", got)
	}
	if strings.Contains(frame.GetError().GetMessage(), "db down") {
		t.Fatalf("текст внутренней ошибки ушёл клиенту: %q", frame.GetError().GetMessage())
	}
}

func TestBadEvents(t *testing.T) {
	f := newFixture()
	body := echoFrame(t, []byte("x"), false)

	wantEmpty(t, f.event(t, eventMessage, "", binaryContentType, body), http.StatusBadRequest)
	wantEmpty(t, f.event(t, "", "c1", binaryContentType, body), http.StatusBadRequest)
	wantEmpty(t, f.event(t, "PING", "c1", binaryContentType, body), http.StatusBadRequest)

	tooLarge := make([]byte, maxMessageBytes+1)
	wantEmpty(t, f.event(t, eventMessage, "c1", binaryContentType, tooLarge), http.StatusRequestEntityTooLarge)
}

func TestPingRoute(t *testing.T) {
	f := newFixture()
	srv := httptest.NewServer(f.handler)
	defer srv.Close()
	client := coffeechatv1connect.NewSystemServiceClient(srv.Client(), srv.URL)

	res, err := client.Ping(context.Background(), connect.NewRequest(&coffeechatv1.PingRequest{Nonce: []byte("n")}))
	if err != nil {
		t.Fatalf("Ping: %v", err)
	}
	if !bytes.Equal(res.Msg.GetNonce(), []byte("n")) || !res.Msg.GetStoreOk() {
		t.Fatalf("ответ %v", res.Msg)
	}
	// CORS — дело шлюза: контейнер этот заголовок не ставит.
	if got := res.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("Access-Control-Allow-Origin = %q, ожидался пустой", got)
	}
}
