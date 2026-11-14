// Package core содержит бизнес-логику сервера. Об инфраструктуре ядро не знает:
// с базой и соединениями оно работает только через интерфейсы пакета port.
package core

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"google.golang.org/protobuf/proto"

	coffeechatv1 "git.sourcecraft.dev/drflamov/coffeechat/server/gen/coffeechat/v1"
	"git.sourcecraft.dev/drflamov/coffeechat/server/internal/port"
)

// MaxNonceLen — наибольшая длина nonce в Ping и Echo, в байтах.
const MaxNonceLen = 32

// connectionTTL — срок жизни записи о соединении: шлюз держит соединение
// не дольше 60 минут, ещё 10 минут — запас.
const connectionTTL = 70 * time.Minute

// Коды ошибок в кадре Error.
const (
	codeBadFrame = "bad_frame"
	codeTooLarge = "too_large"
)

// ErrNonceTooLarge — nonce длиннее MaxNonceLen. Точка входа HTTP превращает её в invalid_argument.
var ErrNonceTooLarge = errors.New("nonce longer than 32 bytes")

// PingResult — ответ на Ping.
type PingResult struct {
	Nonce      []byte
	Version    string
	ServerTime time.Time
	StoreOK    bool
}

// System — служебная логика этапа 0: проверка живости и эхо по WebSocket.
// Поля после создания не меняются, поэтому методы можно вызывать одновременно.
type System struct {
	store     port.Store
	transport port.Transport
	version   string
	now       func() time.Time
}

// NewSystem создаёт ядро. now — источник времени: в бою time.Now, в тестах подставные часы.
func NewSystem(store port.Store, transport port.Transport, version string, now func() time.Time) *System {
	return &System{store: store, transport: transport, version: version, now: now}
}

// Ping возвращает nonce, версию и время сервера. Недоступная база — не ошибка:
// она видна как StoreOK = false, чтобы страница показала, что именно сломано.
func (s *System) Ping(ctx context.Context, nonce []byte) (PingResult, error) {
	if len(nonce) > MaxNonceLen {
		return PingResult{}, ErrNonceTooLarge
	}
	storeOK := true
	if err := s.store.Ping(ctx); err != nil {
		storeOK = false
		slog.WarnContext(ctx, "store ping failed", "error", err)
	}
	return PingResult{
		Nonce:      nonce,
		Version:    s.version,
		ServerTime: s.now(),
		StoreOK:    storeOK,
	}, nil
}

// OnConnect запоминает открытое соединение. Повтор с тем же ID перезаписывает запись.
func (s *System) OnConnect(ctx context.Context, connID string) error {
	now := s.now()
	err := s.store.PutConnection(ctx, port.Connection{
		ID:          connID,
		ConnectedAt: now,
		ExpiresAt:   now.Add(connectionTTL),
	})
	if err != nil {
		return fmt.Errorf("put connection: %w", err)
	}
	return nil
}

// OnDisconnect забывает соединение. Повтор и отсутствие записи — не ошибка.
func (s *System) OnDisconnect(ctx context.Context, connID string) error {
	if err := s.store.DeleteConnection(ctx, connID); err != nil {
		return fmt.Errorf("delete connection: %w", err)
	}
	return nil
}

// OnMessage обрабатывает двоичный кадр ClientFrame.
// Возвращает кадр ServerFrame, который надо отдать телом ответа, или nil, если отдавать нечего.
// Ошибка означает сбой базы или транспорта; неверный кадр клиента — это ответ Error, а не ошибка.
func (s *System) OnMessage(ctx context.Context, connID string, frame []byte) ([]byte, error) {
	var in coffeechatv1.ClientFrame
	if err := proto.Unmarshal(frame, &in); err != nil {
		return errorFrame(codeBadFrame, "frame is not a ClientFrame")
	}
	switch payload := in.GetPayload().(type) {
	case *coffeechatv1.ClientFrame_Echo:
		return s.onEcho(ctx, connID, payload.Echo)
	default:
		// Пустой кадр или тип из более новой версии протокола.
		return errorFrame(codeBadFrame, "unknown or empty payload")
	}
}

func (s *System) onEcho(ctx context.Context, connID string, echo *coffeechatv1.Echo) ([]byte, error) {
	if len(echo.GetNonce()) > MaxNonceLen {
		return errorFrame(codeTooLarge, ErrNonceTooLarge.Error())
	}
	reply, err := proto.Marshal(&coffeechatv1.ServerFrame{
		Payload: &coffeechatv1.ServerFrame_Echo{
			Echo: &coffeechatv1.Echo{Nonce: echo.GetNonce(), ViaSendApi: echo.GetViaSendApi()},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("marshal echo: %w", err)
	}
	if !echo.GetViaSendApi() {
		return reply, nil
	}

	// Ответ через Send API: сначала убеждаемся, что соединение нам известно.
	if _, err := s.store.GetConnection(ctx, connID); err != nil {
		if errors.Is(err, port.ErrNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("get connection: %w", err)
	}
	if err := s.transport.Send(ctx, connID, reply); err != nil {
		if !errors.Is(err, port.ErrGone) {
			return nil, fmt.Errorf("send echo: %w", err)
		}
		// Соединение уже закрыто: запись больше не нужна, для клиента это не ошибка.
		if err := s.store.DeleteConnection(ctx, connID); err != nil {
			return nil, fmt.Errorf("delete gone connection: %w", err)
		}
	}
	return nil, nil
}

// errorFrame собирает ответ ServerFrame с ошибкой для клиента.
func errorFrame(code, message string) ([]byte, error) {
	data, err := proto.Marshal(&coffeechatv1.ServerFrame{
		Payload: &coffeechatv1.ServerFrame_Error{
			Error: &coffeechatv1.Error{Code: code, Message: message},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("marshal error frame: %w", err)
	}
	return data, nil
}
