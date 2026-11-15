package httpapi_test

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/proto"

	coffeechatv1 "git.sourcecraft.dev/drflamov/coffeechat/server/gen/coffeechat/v1"
	"git.sourcecraft.dev/drflamov/coffeechat/server/gen/coffeechat/v1/coffeechatv1connect"
	"git.sourcecraft.dev/drflamov/coffeechat/server/internal/core"
	"git.sourcecraft.dev/drflamov/coffeechat/server/internal/httpapi"
	"git.sourcecraft.dev/drflamov/coffeechat/server/internal/port"
)

// fakeStore — подделка хранилища: отвечает на Ping заданной ошибкой.
type fakeStore struct {
	pingErr error
}

func (s *fakeStore) Ping(context.Context) error                           { return s.pingErr }
func (s *fakeStore) PutConnection(context.Context, port.Connection) error { return nil }
func (s *fakeStore) DeleteConnection(context.Context, string) error       { return nil }
func (s *fakeStore) GetConnection(context.Context, string) (port.Connection, error) {
	return port.Connection{}, port.ErrNotFound
}

type nopTransport struct{}

func (nopTransport) Send(context.Context, string, []byte) error { return nil }

var fixedTime = time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)

func newClient(t *testing.T, store port.Store) coffeechatv1connect.SystemServiceClient {
	t.Helper()
	system := core.NewSystem(store, nopTransport{}, "abc1234", func() time.Time { return fixedTime })
	mux := http.NewServeMux()
	mux.Handle(httpapi.NewSystemHandler(system))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return coffeechatv1connect.NewSystemServiceClient(srv.Client(), srv.URL)
}

func TestPing(t *testing.T) {
	client := newClient(t, &fakeStore{})
	nonce := []byte("0123456789abcdef")

	res, err := client.Ping(context.Background(), connect.NewRequest(&coffeechatv1.PingRequest{Nonce: nonce}))
	if err != nil {
		t.Fatalf("Ping: %v", err)
	}
	if !bytes.Equal(res.Msg.GetNonce(), nonce) {
		t.Errorf("nonce %x, ожидался %x", res.Msg.GetNonce(), nonce)
	}
	if res.Msg.GetVersion() != "abc1234" {
		t.Errorf("version %q", res.Msg.GetVersion())
	}
	if !res.Msg.GetServerTime().AsTime().Equal(fixedTime) {
		t.Errorf("server_time %v", res.Msg.GetServerTime().AsTime())
	}
	if !res.Msg.GetStoreOk() {
		t.Error("store_ok = false, ожидалось true")
	}
}

func TestPingStoreDown(t *testing.T) {
	client := newClient(t, &fakeStore{pingErr: errors.New("db down")})

	res, err := client.Ping(context.Background(), connect.NewRequest(&coffeechatv1.PingRequest{}))
	if err != nil {
		t.Fatalf("Ping: %v", err)
	}
	if res.Msg.GetStoreOk() {
		t.Error("store_ok = true при недоступной базе")
	}
}

func TestPingNonceTooLarge(t *testing.T) {
	client := newClient(t, &fakeStore{})
	nonce := make([]byte, core.MaxNonceLen+1)

	_, err := client.Ping(context.Background(), connect.NewRequest(&coffeechatv1.PingRequest{Nonce: nonce}))
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("ошибка %v, ожидался код invalid_argument", err)
	}
}

func TestErrorFrames(t *testing.T) {
	cases := []struct {
		name  string
		build func() ([]byte, error)
		code  string
	}{
		{"bad_frame", httpapi.BadFrameText, "bad_frame"},
		{"internal", httpapi.InternalError, "internal"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data, err := tc.build()
			if err != nil {
				t.Fatalf("сборка кадра: %v", err)
			}
			var frame coffeechatv1.ServerFrame
			if err := proto.Unmarshal(data, &frame); err != nil {
				t.Fatalf("кадр не разбирается: %v", err)
			}
			if got := frame.GetError().GetCode(); got != tc.code {
				t.Errorf("код %q, ожидался %q", got, tc.code)
			}
		})
	}
}
