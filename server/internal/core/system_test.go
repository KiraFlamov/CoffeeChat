package core_test

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	coffeechatv1 "git.sourcecraft.dev/drflamov/coffeechat/server/gen/coffeechat/v1"
	"git.sourcecraft.dev/drflamov/coffeechat/server/internal/core"
	"git.sourcecraft.dev/drflamov/coffeechat/server/internal/port"
)

// fakeStore — подделка хранилища в памяти.
type fakeStore struct {
	mu        sync.Mutex
	conns     map[string]port.Connection
	pingErr   error
	getErr    error
	deleteErr error
}

func newFakeStore() *fakeStore {
	return &fakeStore{conns: map[string]port.Connection{}}
}

func (s *fakeStore) Ping(context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pingErr
}

func (s *fakeStore) PutConnection(_ context.Context, c port.Connection) error {
	s.mu.Lock()
	defer s.mu.Unlock()
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
	if s.deleteErr != nil {
		return s.deleteErr
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

type sentFrame struct {
	connID string
	data   []byte
}

// fakeTransport — подделка транспорта: запоминает отправленные кадры.
type fakeTransport struct {
	mu   sync.Mutex
	sent []sentFrame
	err  error
}

func (t *fakeTransport) Send(_ context.Context, connID string, data []byte) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.err != nil {
		return t.err
	}
	t.sent = append(t.sent, sentFrame{connID: connID, data: bytes.Clone(data)})
	return nil
}

func (t *fakeTransport) frames() []sentFrame {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]sentFrame(nil), t.sent...)
}

var fixedNow = time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)

func newSystem(store port.Store, transport port.Transport) *core.System {
	return core.NewSystem(store, transport, "abc1234", func() time.Time { return fixedNow })
}

func echoFrame(t *testing.T, nonce []byte, viaSendAPI bool) []byte {
	t.Helper()
	data, err := proto.Marshal(&coffeechatv1.ClientFrame{
		Payload: &coffeechatv1.ClientFrame_Echo{
			Echo: &coffeechatv1.Echo{Nonce: nonce, ViaSendApi: viaSendAPI},
		},
	})
	if err != nil {
		t.Fatalf("marshal ClientFrame: %v", err)
	}
	return data
}

func decodeServerFrame(t *testing.T, data []byte) *coffeechatv1.ServerFrame {
	t.Helper()
	if data == nil {
		t.Fatal("ожидался кадр, получен nil")
	}
	var frame coffeechatv1.ServerFrame
	if err := proto.Unmarshal(data, &frame); err != nil {
		t.Fatalf("unmarshal ServerFrame: %v", err)
	}
	return &frame
}

func TestPingReturnsNonceVersionAndTime(t *testing.T) {
	sys := newSystem(newFakeStore(), &fakeTransport{})
	nonce := []byte{1, 2, 3}

	got, err := sys.Ping(context.Background(), nonce)
	if err != nil {
		t.Fatalf("Ping: %v", err)
	}
	if !bytes.Equal(got.Nonce, nonce) {
		t.Errorf("Nonce = %v, ожидался %v", got.Nonce, nonce)
	}
	if got.Version != "abc1234" {
		t.Errorf("Version = %q", got.Version)
	}
	if !got.ServerTime.Equal(fixedNow) {
		t.Errorf("ServerTime = %v, ожидалось %v", got.ServerTime, fixedNow)
	}
	if !got.StoreOK {
		t.Error("StoreOK = false при рабочей базе")
	}
}

func TestPingStoreDownIsNotAnError(t *testing.T) {
	store := newFakeStore()
	store.pingErr = errors.New("база недоступна")
	sys := newSystem(store, &fakeTransport{})

	got, err := sys.Ping(context.Background(), []byte{1})
	if err != nil {
		t.Fatalf("Ping вернул ошибку: %v", err)
	}
	if got.StoreOK {
		t.Error("StoreOK = true при недоступной базе")
	}
	if !bytes.Equal(got.Nonce, []byte{1}) {
		t.Errorf("Nonce = %v", got.Nonce)
	}
}

func TestPingNonceLimit(t *testing.T) {
	sys := newSystem(newFakeStore(), &fakeTransport{})

	if _, err := sys.Ping(context.Background(), make([]byte, core.MaxNonceLen)); err != nil {
		t.Errorf("nonce ровно %d байт отвергнут: %v", core.MaxNonceLen, err)
	}
	_, err := sys.Ping(context.Background(), make([]byte, core.MaxNonceLen+1))
	if !errors.Is(err, core.ErrNonceTooLarge) {
		t.Errorf("err = %v, ожидалась ErrNonceTooLarge", err)
	}
}

func TestOnConnectStoresConnection(t *testing.T) {
	store := newFakeStore()
	sys := newSystem(store, &fakeTransport{})

	if err := sys.OnConnect(context.Background(), "c1"); err != nil {
		t.Fatalf("OnConnect: %v", err)
	}
	// Повторный CONNECT с тем же ID — не ошибка.
	if err := sys.OnConnect(context.Background(), "c1"); err != nil {
		t.Fatalf("повторный OnConnect: %v", err)
	}
	got, err := store.GetConnection(context.Background(), "c1")
	if err != nil {
		t.Fatalf("GetConnection: %v", err)
	}
	if !got.ConnectedAt.Equal(fixedNow) {
		t.Errorf("ConnectedAt = %v", got.ConnectedAt)
	}
	if want := fixedNow.Add(70 * time.Minute); !got.ExpiresAt.Equal(want) {
		t.Errorf("ExpiresAt = %v, ожидалось %v", got.ExpiresAt, want)
	}
}

func TestOnDisconnectDeletesConnection(t *testing.T) {
	store := newFakeStore()
	sys := newSystem(store, &fakeTransport{})
	ctx := context.Background()

	// DISCONNECT раньше CONNECT — не ошибка.
	if err := sys.OnDisconnect(ctx, "c1"); err != nil {
		t.Fatalf("OnDisconnect без соединения: %v", err)
	}
	if err := sys.OnConnect(ctx, "c1"); err != nil {
		t.Fatalf("OnConnect: %v", err)
	}
	if err := sys.OnDisconnect(ctx, "c1"); err != nil {
		t.Fatalf("OnDisconnect: %v", err)
	}
	if store.has("c1") {
		t.Error("соединение не удалено")
	}
}

func TestOnMessageEchoRepliesInBody(t *testing.T) {
	transport := &fakeTransport{}
	sys := newSystem(newFakeStore(), transport)
	nonce := []byte{9, 8, 7}

	reply, err := sys.OnMessage(context.Background(), "c1", echoFrame(t, nonce, false))
	if err != nil {
		t.Fatalf("OnMessage: %v", err)
	}
	echo := decodeServerFrame(t, reply).GetEcho()
	if echo == nil {
		t.Fatal("в ответе нет Echo")
	}
	if !bytes.Equal(echo.GetNonce(), nonce) {
		t.Errorf("Nonce = %v, ожидался %v", echo.GetNonce(), nonce)
	}
	if echo.GetViaSendApi() {
		t.Error("ViaSendApi = true")
	}
	if n := len(transport.frames()); n != 0 {
		t.Errorf("Transport.Send вызван %d раз, ожидалось 0", n)
	}
}

func TestOnMessageEchoViaSendAPI(t *testing.T) {
	store := newFakeStore()
	transport := &fakeTransport{}
	sys := newSystem(store, transport)
	ctx := context.Background()
	nonce := []byte{4, 5, 6}
	if err := sys.OnConnect(ctx, "c1"); err != nil {
		t.Fatalf("OnConnect: %v", err)
	}

	reply, err := sys.OnMessage(ctx, "c1", echoFrame(t, nonce, true))
	if err != nil {
		t.Fatalf("OnMessage: %v", err)
	}
	if reply != nil {
		t.Errorf("reply = %v, ожидался nil", reply)
	}
	sent := transport.frames()
	if len(sent) != 1 {
		t.Fatalf("Transport.Send вызван %d раз, ожидался 1", len(sent))
	}
	if sent[0].connID != "c1" {
		t.Errorf("connID = %q", sent[0].connID)
	}
	echo := decodeServerFrame(t, sent[0].data).GetEcho()
	if echo == nil || !bytes.Equal(echo.GetNonce(), nonce) || !echo.GetViaSendApi() {
		t.Errorf("отправлен неверный кадр: %v", echo)
	}
}

func TestOnMessageSendAPIGoneDeletesConnection(t *testing.T) {
	store := newFakeStore()
	transport := &fakeTransport{err: port.ErrGone}
	sys := newSystem(store, transport)
	ctx := context.Background()
	if err := sys.OnConnect(ctx, "c1"); err != nil {
		t.Fatalf("OnConnect: %v", err)
	}

	reply, err := sys.OnMessage(ctx, "c1", echoFrame(t, []byte{1}, true))
	if err != nil {
		t.Fatalf("OnMessage вернул ошибку: %v", err)
	}
	if reply != nil {
		t.Errorf("reply = %v, ожидался nil", reply)
	}
	if store.has("c1") {
		t.Error("закрытое соединение не удалено")
	}
}

func TestOnMessageSendAPIUnknownConnection(t *testing.T) {
	transport := &fakeTransport{}
	sys := newSystem(newFakeStore(), transport)

	// Записи о соединении нет (не было CONNECT или запись просрочена).
	reply, err := sys.OnMessage(context.Background(), "c1", echoFrame(t, []byte{1}, true))
	if err != nil {
		t.Fatalf("OnMessage вернул ошибку: %v", err)
	}
	if reply != nil {
		t.Errorf("reply = %v, ожидался nil", reply)
	}
	if n := len(transport.frames()); n != 0 {
		t.Errorf("Transport.Send вызван %d раз, ожидалось 0", n)
	}
}

func TestOnMessageInfrastructureErrors(t *testing.T) {
	boom := errors.New("сбой")
	tests := []struct {
		name  string
		setup func(*fakeStore, *fakeTransport)
	}{
		{"GetConnection", func(s *fakeStore, _ *fakeTransport) { s.getErr = boom }},
		{"Send", func(_ *fakeStore, tr *fakeTransport) { tr.err = boom }},
		{"DeleteConnection после ErrGone", func(s *fakeStore, tr *fakeTransport) {
			tr.err = port.ErrGone
			s.deleteErr = boom
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newFakeStore()
			transport := &fakeTransport{}
			sys := newSystem(store, transport)
			ctx := context.Background()
			if err := sys.OnConnect(ctx, "c1"); err != nil {
				t.Fatalf("OnConnect: %v", err)
			}
			tt.setup(store, transport)

			reply, err := sys.OnMessage(ctx, "c1", echoFrame(t, []byte{1}, true))
			if !errors.Is(err, boom) {
				t.Errorf("err = %v, ожидалась обёрнутая %v", err, boom)
			}
			if reply != nil {
				t.Errorf("reply = %v, ожидался nil", reply)
			}
		})
	}
}

func TestOnMessageBadFrame(t *testing.T) {
	tests := []struct {
		name  string
		frame []byte
	}{
		{"мусор", []byte{0xff, 0xff, 0xff}},
		{"пустой кадр", nil},
		{"кадр без payload", []byte{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sys := newSystem(newFakeStore(), &fakeTransport{})

			reply, err := sys.OnMessage(context.Background(), "c1", tt.frame)
			if err != nil {
				t.Fatalf("OnMessage вернул ошибку: %v", err)
			}
			if code := decodeServerFrame(t, reply).GetError().GetCode(); code != "bad_frame" {
				t.Errorf("code = %q, ожидался bad_frame", code)
			}
		})
	}
}

func TestOnMessageNonceTooLarge(t *testing.T) {
	for _, viaSendAPI := range []bool{false, true} {
		transport := &fakeTransport{}
		sys := newSystem(newFakeStore(), transport)

		frame := echoFrame(t, make([]byte, core.MaxNonceLen+1), viaSendAPI)
		reply, err := sys.OnMessage(context.Background(), "c1", frame)
		if err != nil {
			t.Fatalf("OnMessage вернул ошибку: %v", err)
		}
		if code := decodeServerFrame(t, reply).GetError().GetCode(); code != "too_large" {
			t.Errorf("code = %q, ожидался too_large", code)
		}
		if n := len(transport.frames()); n != 0 {
			t.Errorf("Transport.Send вызван %d раз, ожидалось 0", n)
		}
	}
}

// Один экземпляр System обслуживает запросы одновременно (инвариант 6).
// Тест имеет смысл с детектором гонок: task test:go:race.
func TestSystemIsSafeForConcurrentUse(t *testing.T) {
	sys := newSystem(newFakeStore(), &fakeTransport{})
	ctx := context.Background()
	frame := echoFrame(t, []byte{1}, true)

	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = sys.OnConnect(ctx, "c1")
			_, _ = sys.Ping(ctx, []byte{1})
			_, _ = sys.OnMessage(ctx, "c1", frame)
			_ = sys.OnDisconnect(ctx, "c1")
		}()
	}
	wg.Wait()
}
