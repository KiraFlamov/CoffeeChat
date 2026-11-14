// Package ydb — адаптер порта Store для YDB (облачная serverless-база и локальный local-ydb).
package ydb

import (
	"context"
	"errors"
	"fmt"
	"time"

	sdk "github.com/ydb-platform/ydb-go-sdk/v3"
	"github.com/ydb-platform/ydb-go-sdk/v3/query"

	"git.sourcecraft.dev/drflamov/coffeechat/server/internal/port"
)

// Store хранит состояние сервера в YDB. Методы можно вызывать одновременно.
type Store struct {
	driver *sdk.Driver
}

var _ port.Store = (*Store)(nil)

// Open подключается к базе. dsn — строка вида grpc://localhost:2136/local.
// opts — дополнительные настройки драйвера, например способ аутентификации в облаке.
// Подключение создаётся один раз при старте процесса и закрывается через Close.
func Open(ctx context.Context, dsn string, opts ...sdk.Option) (*Store, error) {
	driver, err := sdk.Open(ctx, dsn, opts...)
	if err != nil {
		return nil, fmt.Errorf("open ydb: %w", err)
	}
	return &Store{driver: driver}, nil
}

// Close закрывает подключение.
func (s *Store) Close(ctx context.Context) error {
	if err := s.driver.Close(ctx); err != nil {
		return fmt.Errorf("close ydb: %w", err)
	}
	return nil
}

// Все запросы идемпотентны, поэтому SDK сам повторяет их при временных ошибках
// в пределах срока контекста.

// Ping проверяет доступность базы одним дешёвым запросом.
func (s *Store) Ping(ctx context.Context) error {
	if err := s.driver.Query().Exec(ctx, `SELECT 1;`, query.WithIdempotent()); err != nil {
		return fmt.Errorf("ping: %w", err)
	}
	return nil
}

// PutConnection сохраняет соединение. Повтор с тем же ID перезаписывает запись.
// YDB хранит время с точностью до микросекунды.
func (s *Store) PutConnection(ctx context.Context, c port.Connection) error {
	err := s.driver.Query().Exec(ctx, `
DECLARE $id AS Utf8;
DECLARE $connected_at AS Timestamp;
DECLARE $expires_at AS Timestamp;
UPSERT INTO connections (connection_id, connected_at, expires_at)
VALUES ($id, $connected_at, $expires_at);`,
		query.WithParameters(sdk.ParamsBuilder().
			Param("$id").Text(c.ID).
			Param("$connected_at").Timestamp(c.ConnectedAt).
			Param("$expires_at").Timestamp(c.ExpiresAt).
			Build()),
		query.WithIdempotent(),
	)
	if err != nil {
		return fmt.Errorf("put connection: %w", err)
	}
	return nil
}

// GetConnection возвращает port.ErrNotFound, если записи нет или она просрочена.
// Срок сверяется с часами базы: удаление по TTL фоновое и не мгновенное.
func (s *Store) GetConnection(ctx context.Context, id string) (port.Connection, error) {
	row, err := s.driver.Query().QueryRow(ctx, `
DECLARE $id AS Utf8;
SELECT connection_id, connected_at, expires_at
FROM connections
WHERE connection_id = $id AND expires_at > CurrentUtcTimestamp();`,
		query.WithParameters(sdk.ParamsBuilder().Param("$id").Text(id).Build()),
		query.WithIdempotent(),
	)
	if err != nil {
		if errors.Is(err, query.ErrNoRows) {
			return port.Connection{}, port.ErrNotFound
		}
		return port.Connection{}, fmt.Errorf("get connection: %w", err)
	}
	var (
		c                      port.Connection
		connectedAt, expiresAt time.Time
	)
	if err := row.Scan(&c.ID, &connectedAt, &expiresAt); err != nil {
		return port.Connection{}, fmt.Errorf("scan connection: %w", err)
	}
	c.ConnectedAt = connectedAt.UTC()
	c.ExpiresAt = expiresAt.UTC()
	return c, nil
}

// DeleteConnection удаляет запись. Отсутствие записи — не ошибка.
func (s *Store) DeleteConnection(ctx context.Context, id string) error {
	err := s.driver.Query().Exec(ctx, `
DECLARE $id AS Utf8;
DELETE FROM connections WHERE connection_id = $id;`,
		query.WithParameters(sdk.ParamsBuilder().Param("$id").Text(id).Build()),
		query.WithIdempotent(),
	)
	if err != nil {
		return fmt.Errorf("delete connection: %w", err)
	}
	return nil
}
