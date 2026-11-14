package port

import (
	"context"
	"errors"
)

// ErrGone — соединение закрыто, отправлять некуда.
var ErrGone = errors.New("connection gone")

// Transport доставляет кадры в открытые соединения WebSocket.
type Transport interface {
	// Send отправляет двоичный кадр в соединение.
	Send(ctx context.Context, connectionID string, data []byte) error
}
