// Package hub — адаптер порта Transport для локального сервера: соединения
// WebSocket живут в памяти одного процесса. В облаке вместо него transport/yc.
package hub

import (
	"context"
	"fmt"
	"sync"

	"git.sourcecraft.dev/drflamov/coffeechat/server/internal/port"
)

// SendFunc пишет один двоичный кадр в соединение.
type SendFunc func(ctx context.Context, data []byte) error

// Hub помнит открытые соединения и отправляет в них кадры.
// Методы можно вызывать одновременно.
type Hub struct {
	mu    sync.RWMutex
	conns map[string]SendFunc
}

var _ port.Transport = (*Hub)(nil)

// New создаёт пустой хаб.
func New() *Hub {
	return &Hub{conns: map[string]SendFunc{}}
}

// Register запоминает соединение. Повтор с тем же ID заменяет прежнюю запись.
func (h *Hub) Register(connectionID string, send SendFunc) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.conns[connectionID] = send
}

// Unregister забывает соединение. Отсутствие записи — не ошибка.
func (h *Hub) Unregister(connectionID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.conns, connectionID)
}

// Send отправляет двоичный кадр в соединение.
// Неизвестное или уже закрытое соединение — port.ErrGone.
func (h *Hub) Send(ctx context.Context, connectionID string, data []byte) error {
	h.mu.RLock()
	send, ok := h.conns[connectionID]
	h.mu.RUnlock()
	if !ok {
		return port.ErrGone
	}
	// Запись идёт без блокировки хаба: медленный клиент не задерживает остальных.
	if err := send(ctx, data); err != nil {
		if ctx.Err() != nil {
			// Вышел срок запроса: о состоянии соединения это ничего не говорит.
			return fmt.Errorf("hub send: %w", err)
		}
		// Запись не удалась — соединение закрыто или сломано, отправлять некуда.
		return port.ErrGone
	}
	return nil
}
