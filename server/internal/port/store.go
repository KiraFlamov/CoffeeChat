// Package port описывает интерфейсы, через которые ядро работает с инфраструктурой.
package port

import (
	"context"
	"errors"
	"time"
)

// Connection — открытое соединение WebSocket.
type Connection struct {
	ID          string
	ConnectedAt time.Time
	ExpiresAt   time.Time
}

// ErrNotFound — записи нет или она просрочена.
var ErrNotFound = errors.New("not found")

// Store — хранилище состояния сервера.
type Store interface {
	// Ping проверяет доступность базы одним дешёвым запросом.
	Ping(ctx context.Context) error
	// PutConnection сохраняет соединение. Повтор с тем же ID перезаписывает запись.
	PutConnection(ctx context.Context, c Connection) error
	// GetConnection возвращает ErrNotFound, если записи нет или она просрочена.
	GetConnection(ctx context.Context, id string) (Connection, error)
	// DeleteConnection удаляет запись. Отсутствие записи — не ошибка.
	DeleteConnection(ctx context.Context, id string) error
}
