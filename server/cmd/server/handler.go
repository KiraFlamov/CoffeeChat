package main

import (
	"context"
	"crypto/rand"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/coder/websocket"

	"git.sourcecraft.dev/drflamov/coffeechat/server/internal/core"
	"git.sourcecraft.dev/drflamov/coffeechat/server/internal/httpapi"
	"git.sourcecraft.dev/drflamov/coffeechat/server/internal/transport/hub"
)

const (
	// maxMessageBytes — предел сообщения WebSocket, как у API Gateway (128 КБ).
	// Превышение закрывает соединение с кодом 1009.
	maxMessageBytes = 128 << 10
	// messageTimeout — срок обработки одного сообщения, как таймаут запроса в облаке.
	messageTimeout = 10 * time.Second
	// corsMaxAge — на сколько секунд браузер запоминает ответ на preflight.
	corsMaxAge = "7200"
)

// newHandler собирает маршруты локального сервера: HTTP API и WebSocket.
// origin — единственный адрес фронтенда, которому разрешён доступ из браузера.
func newHandler(system *core.System, connections *hub.Hub, origin *url.URL) http.Handler {
	mux := http.NewServeMux()
	path, api := httpapi.NewSystemHandler(system)
	mux.Handle(path, withCORS(origin.String(), api))
	mux.Handle("GET /ws", &wsHandler{system: system, hub: connections, originHost: origin.Host})
	return mux
}

// withCORS разрешает запросы HTTP API со страницы фронтенда. В облаке это делает
// API Gateway; поведение то же: в ответе всегда один наш origin, без «*»,
// поэтому чужому сайту браузер ответ не отдаст.
func withCORS(origin string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Access-Control-Allow-Origin", origin)
		// Preflight: браузер спрашивает разрешение перед настоящим запросом.
		if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
			h.Set("Access-Control-Allow-Methods", http.MethodPost)
			// Заголовки, которые шлёт клиент ConnectRPC.
			h.Set("Access-Control-Allow-Headers", "Content-Type, Connect-Protocol-Version, Connect-Timeout-Ms")
			h.Set("Access-Control-Max-Age", corsMaxAge)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// wsHandler обслуживает одно соединение WebSocket: то, что в облаке делают
// API Gateway и обработчик его событий CONNECT, MESSAGE и DISCONNECT.
type wsHandler struct {
	system     *core.System
	hub        *hub.Hub
	originHost string
}

func (h *wsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	// ID соединения в облаке выдаёт шлюз, локально — мы сами.
	connID := rand.Text()
	log := slog.With("connection_id", connID)

	// Как в облаке: если соединение не удалось записать в базу, оно не открывается.
	if err := h.system.OnConnect(ctx, connID); err != nil {
		log.ErrorContext(ctx, "ws connect failed", "error", err)
		http.Error(w, "service unavailable", http.StatusServiceUnavailable)
		return
	}
	defer func() {
		// Контекст запроса к этому моменту уже отменён.
		ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), messageTimeout)
		defer cancel()
		if err := h.system.OnDisconnect(ctx, connID); err != nil {
			log.ErrorContext(ctx, "ws disconnect failed", "error", err)
		}
	}()

	// Браузеру разрешён только наш origin; клиенты без заголовка Origin
	// (websocat, тесты) проходят.
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: []string{h.originHost}})
	if err != nil {
		log.WarnContext(ctx, "ws handshake rejected", "error", err)
		return
	}
	defer func() { _ = conn.CloseNow() }()
	conn.SetReadLimit(maxMessageBytes)

	h.hub.Register(connID, func(ctx context.Context, data []byte) error {
		return conn.Write(ctx, websocket.MessageBinary, data)
	})
	defer h.hub.Unregister(connID)
	log.InfoContext(ctx, "ws connected")

	for {
		typ, data, err := conn.Read(ctx)
		if err != nil {
			log.InfoContext(ctx, "ws closed", "status", int(websocket.CloseStatus(err)))
			return
		}
		reply := h.onMessage(ctx, log, connID, typ, data)
		if reply == nil {
			continue
		}
		if err := h.write(ctx, conn, reply); err != nil {
			log.InfoContext(ctx, "ws write failed", "error", err)
			return
		}
	}
}

// onMessage возвращает кадр, который надо отправить клиенту в ответ, или nil.
func (h *wsHandler) onMessage(
	ctx context.Context, log *slog.Logger, connID string, typ websocket.MessageType, data []byte,
) []byte {
	ctx, cancel := context.WithTimeout(ctx, messageTimeout)
	defer cancel()

	if typ != websocket.MessageBinary {
		// Протокол только двоичный.
		return mustFrame(ctx, log, httpapi.BadFrameText)
	}
	reply, err := h.system.OnMessage(ctx, connID, data)
	if err != nil {
		log.ErrorContext(ctx, "ws message failed", "size", len(data), "error", err)
		return mustFrame(ctx, log, httpapi.InternalError)
	}
	return reply
}

func (h *wsHandler) write(ctx context.Context, conn *websocket.Conn, data []byte) error {
	ctx, cancel := context.WithTimeout(ctx, messageTimeout)
	defer cancel()
	return conn.Write(ctx, websocket.MessageBinary, data)
}

// mustFrame собирает служебный кадр. Сборка не может упасть на деле;
// если упала — клиент просто не получит ответа.
func mustFrame(ctx context.Context, log *slog.Logger, build func() ([]byte, error)) []byte {
	data, err := build()
	if err != nil {
		log.ErrorContext(ctx, "build error frame failed", "error", err)
		return nil
	}
	return data
}
