package ydb

import (
	"context"
	"fmt"
	"time"

	sdk "github.com/ydb-platform/ydb-go-sdk/v3"
)

const (
	// readyRetryInterval — пауза между попытками подключиться к ещё не готовой базе.
	readyRetryInterval = 2 * time.Second
	// readyAttemptTimeout — срок одной попытки: подключение и запрос к базе,
	// которая ещё стартует, могут зависнуть.
	readyAttemptTimeout = 10 * time.Second
)

// OpenWhenReady подключается к базе и ждёт, пока она начнёт отвечать: база
// в контейнере стартует не мгновенно. Срок ожидания задаёт ctx.
// onRetry вызывается перед каждой повторной попыткой; может быть nil.
func OpenWhenReady(ctx context.Context, dsn string, onRetry func(), opts ...sdk.Option) (*Store, error) {
	for {
		store, err := tryOpen(ctx, dsn, opts...)
		if err == nil {
			return store, nil
		}
		if onRetry != nil {
			onRetry()
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("database is not ready: %w", err)
		case <-time.After(readyRetryInterval):
		}
	}
}

func tryOpen(ctx context.Context, dsn string, opts ...sdk.Option) (*Store, error) {
	ctx, cancel := context.WithTimeout(ctx, readyAttemptTimeout)
	defer cancel()
	store, err := Open(ctx, dsn, opts...)
	if err != nil {
		return nil, err
	}
	if err := store.Ping(ctx); err != nil {
		_ = store.Close(ctx)
		return nil, err
	}
	return store, nil
}
