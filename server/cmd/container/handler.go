package main

import (
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"

	"git.sourcecraft.dev/drflamov/coffeechat/server/internal/core"
	"git.sourcecraft.dev/drflamov/coffeechat/server/internal/httpapi"
)

// Заголовки, в которых API Gateway передаёт событие WebSocket (ADR-002).
const (
	headerEventType    = "X-Yc-Apigateway-Websocket-Event-Type"
	headerConnectionID = "X-Yc-Apigateway-Websocket-Connection-Id"
)

// Типы событий шлюза.
const (
	eventConnect    = "CONNECT"
	eventMessage    = "MESSAGE"
	eventDisconnect = "DISCONNECT"
)

const (
	// maxMessageBytes — предел сообщения WebSocket у API Gateway (128 КБ).
	// Шлюз сам закрывает соединение при превышении; здесь — защита на случай обхода.
	maxMessageBytes = 128 << 10
	// binaryContentType — с таким типом шлюз присылает двоичный кадр и по нему же
	// отправляет тело ответа клиенту двоичным кадром.
	binaryContentType = "application/octet-stream"
)

// newHandler собирает маршруты контейнера: HTTP API и события WebSocket от шлюза.
// CORS здесь нет: на preflight отвечает сам шлюз.
func newHandler(system *core.System) http.Handler {
	mux := http.NewServeMux()
	mux.Handle(httpapi.NewSystemHandler(system))
	mux.Handle("/ws", &gatewayHandler{system: system})
	return mux
}

// gatewayHandler принимает события WebSocket. Шлюз держит соединение с клиентом сам,
// а контейнеру присылает каждое событие отдельным HTTP-запросом.
type gatewayHandler struct {
	system *core.System
}

func (h *gatewayHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	event := r.Header.Get(headerEventType)
	connID := r.Header.Get(headerConnectionID)
	if connID == "" {
		http.Error(w, "missing connection id", http.StatusBadRequest)
		return
	}
	log := slog.With("connection_id", connID, "event", event)

	switch event {
	case eventConnect:
		// Ошибка здесь — отказ в подключении: шлюз не откроет соединение.
		if err := h.system.OnConnect(ctx, connID); err != nil {
			log.ErrorContext(ctx, "ws connect failed", "error", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		log.InfoContext(ctx, "ws connected")

	case eventDisconnect:
		if err := h.system.OnDisconnect(ctx, connID); err != nil {
			// Запись всё равно исчезнет по TTL.
			log.ErrorContext(ctx, "ws disconnect failed", "error", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		log.InfoContext(ctx, "ws disconnected")

	case eventMessage:
		h.onMessage(w, r, connID, log)

	default:
		http.Error(w, "unknown event type", http.StatusBadRequest)
	}
}

// onMessage отвечает на сообщение клиента. Тело ответа шлюз отправляет клиенту
// отдельным кадром; при пустом теле клиент не получает ничего.
func (h *gatewayHandler) onMessage(w http.ResponseWriter, r *http.Request, connID string, log *slog.Logger) {
	ctx := r.Context()
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxMessageBytes))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			http.Error(w, "message too large", http.StatusRequestEntityTooLarge)
			return
		}
		log.WarnContext(ctx, "ws read body failed", "error", err)
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	// Текстовый кадр шлюз присылает с Content-Type: application/json. Протокол только двоичный.
	if mediaType, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); mediaType != binaryContentType {
		writeFrame(w, r, log, httpapi.BadFrameText)
		return
	}

	reply, err := h.system.OnMessage(ctx, connID, data)
	if err != nil {
		// Сбой базы или транспорта. Отвечаем кодом 200 с кадром Error: так клиент
		// узнаёт об ошибке, а подробности остаются в журнале.
		log.ErrorContext(ctx, "ws message failed", "size", len(data), "error", err)
		writeFrame(w, r, log, httpapi.InternalError)
		return
	}
	if reply == nil {
		return // ответ ушёл через Send API или отвечать нечего
	}
	writeBinary(w, r, log, reply)
}

func writeFrame(w http.ResponseWriter, r *http.Request, log *slog.Logger, build func() ([]byte, error)) {
	data, err := build()
	if err != nil {
		log.ErrorContext(r.Context(), "build error frame failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeBinary(w, r, log, data)
}

func writeBinary(w http.ResponseWriter, r *http.Request, log *slog.Logger, data []byte) {
	w.Header().Set("Content-Type", binaryContentType)
	// Тело — двоичный кадр Protobuf с типом application/octet-stream, не HTML.
	if _, err := w.Write(data); err != nil { //nolint:gosec // G705: см. комментарий выше
		log.WarnContext(r.Context(), "ws write reply failed", "error", err)
	}
}
