package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"google.golang.org/protobuf/proto"

	coffeechatv1 "git.sourcecraft.dev/drflamov/coffeechat/server/gen/coffeechat/v1"
	"git.sourcecraft.dev/drflamov/coffeechat/server/internal/core"
	"git.sourcecraft.dev/drflamov/coffeechat/server/internal/httpapi"
	"git.sourcecraft.dev/drflamov/coffeechat/server/internal/port"
)

// stand — подставной стенд: API, WebSocket-эхо и «фронтенд» в одном HTTP-сервере.
type stand struct {
	storeErr    error  // ошибка базы: Ping вернёт store_ok = false
	allowOrigin string // что сервер пишет в Access-Control-Allow-Origin; "echo" — origin запроса
	dropEcho    bool   // не отвечать на Echo
	page        string // тело страницы фронтенда
	noAssets    bool   // файлы сборки не отдаются (код 404)
	assetType   string // Content-Type скрипта; по умолчанию text/javascript
}

func (s *stand) Ping(context.Context) error                           { return s.storeErr }
func (s *stand) PutConnection(context.Context, port.Connection) error { return nil }
func (s *stand) DeleteConnection(context.Context, string) error       { return nil }
func (s *stand) GetConnection(context.Context, string) (port.Connection, error) {
	return port.Connection{}, port.ErrNotFound
}
func (s *stand) Send(context.Context, string, []byte) error { return nil }

func (s *stand) start(t *testing.T) config {
	t.Helper()
	srv := httptest.NewUnstartedServer(nil)
	mux := http.NewServeMux()
	path, api := httpapi.NewSystemHandler(core.NewSystem(s, s, "test", time.Now))
	mux.Handle(path, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		allow := s.allowOrigin
		if allow == "echo" {
			allow = r.Header.Get("Origin")
		}
		if allow == "" {
			allow = "http://" + srv.Listener.Addr().String()
		}
		w.Header().Set("Access-Control-Allow-Origin", allow)
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		api.ServeHTTP(w, r)
	}))
	mux.HandleFunc("GET /ws", s.serveWS)
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(s.page))
	})
	mux.HandleFunc("GET /assets/", func(w http.ResponseWriter, r *http.Request) {
		if s.noAssets {
			http.NotFound(w, r)
			return
		}
		contentType := "text/css"
		if strings.HasSuffix(r.URL.Path, ".js") {
			contentType = "text/javascript"
			if s.assetType != "" {
				contentType = s.assetType
			}
		}
		w.Header().Set("Content-Type", contentType)
		_, _ = w.Write([]byte("/* asset */"))
	})
	srv.Config.Handler = mux
	srv.Start()
	t.Cleanup(srv.Close)
	return config{
		apiURL: srv.URL,
		wsURL:  srv.URL + "/ws",
		webURL: srv.URL,
	}
}

// serveWS отвечает на Echo тем же Echo, как настоящий сервер.
func (s *stand) serveWS(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer func() { _ = conn.CloseNow() }()
	for {
		_, data, err := conn.Read(r.Context())
		if err != nil {
			return
		}
		if s.dropEcho {
			continue
		}
		var in coffeechatv1.ClientFrame
		if err := proto.Unmarshal(data, &in); err != nil {
			return
		}
		out, err := proto.Marshal(&coffeechatv1.ServerFrame{
			Payload: &coffeechatv1.ServerFrame_Echo{Echo: in.GetEcho()},
		})
		if err != nil {
			return
		}
		if err := conn.Write(r.Context(), websocket.MessageBinary, out); err != nil {
			return
		}
	}
}

const goodPage = `<!doctype html>
<script type="module" crossorigin src="/assets/index-abc.js"></script>
<link rel="stylesheet" crossorigin href="/assets/index-abc.css">
<div id="root"></div>`

func runSmoke(t *testing.T, cfg config) (bool, string) {
	t.Helper()
	var out bytes.Buffer
	ok := run(context.Background(), cfg, &out)
	return ok, out.String()
}

func TestSmokeAllGreen(t *testing.T) {
	cfg := (&stand{page: goodPage}).start(t)
	ok, report := runSmoke(t, cfg)
	if !ok {
		t.Fatalf("дымовой тест упал на исправном стенде:\n%s", report)
	}
	if got := strings.Count(report, "\n"); got != 6 {
		t.Errorf("строк в отчёте %d, ожидалось 6:\n%s", got, report)
	}
}

func TestSmokeWithoutWeb(t *testing.T) {
	cfg := (&stand{page: goodPage}).start(t)
	cfg.webURL = ""
	ok, report := runSmoke(t, cfg)
	if !ok {
		t.Fatalf("дымовой тест упал:\n%s", report)
	}
	if got := strings.Count(report, "\n"); got != 3 {
		t.Errorf("строк в отчёте %d, ожидалось 3:\n%s", got, report)
	}
}

func TestSmokeFailures(t *testing.T) {
	cases := []struct {
		name  string
		stand stand
		want  string // часть строки отчёта с ошибкой
	}{
		{"база недоступна", stand{page: goodPage, storeErr: context.DeadlineExceeded}, "store_ok = false"},
		{"чужой origin разрешён", stand{page: goodPage, allowOrigin: "echo"}, "равен чужому origin"},
		{"не страница приложения", stand{page: "<h1>bucket listing</h1>"}, "нет элемента #root"},
		{"страница без файлов сборки", stand{page: `<div id="root"></div>`}, "нет ссылок на скрипты"},
		{"файлы сборки не отдаются", stand{page: goodPage, noAssets: true}, "код ответа 404"},
		{"скрипт с неверным типом", stand{page: goodPage, assetType: "application/octet-stream"}, "ожидался тип javascript"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := tc.stand.start(t)
			ok, report := runSmoke(t, cfg)
			if ok {
				t.Fatalf("дымовой тест прошёл на сломанном стенде:\n%s", report)
			}
			if !strings.Contains(report, tc.want) {
				t.Errorf("в отчёте нет %q:\n%s", tc.want, report)
			}
		})
	}
}

func TestSmokeWildcardOriginIsWarning(t *testing.T) {
	cfg := (&stand{page: goodPage, allowOrigin: "*"}).start(t)
	ok, report := runSmoke(t, cfg)
	if !ok {
		t.Fatalf("дымовой тест упал:\n%s", report)
	}
	if !strings.Contains(report, "ВНИМАНИЕ") {
		t.Errorf("в отчёте нет предупреждения про «*»:\n%s", report)
	}
}

func TestSmokeUnreachableStand(t *testing.T) {
	cfg := (&stand{page: goodPage}).start(t)
	cfg.apiURL = "http://127.0.0.1:1"
	cfg.wsURL = "http://127.0.0.1:1/ws"
	ok, report := runSmoke(t, cfg)
	if ok {
		t.Fatalf("дымовой тест прошёл на недоступном стенде:\n%s", report)
	}
}
