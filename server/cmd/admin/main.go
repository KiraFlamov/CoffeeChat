// Команда admin — служебные операции над стендом. Пока одна: применить миграции базы.
//
//	admin migrate
//
// Строка подключения берётся из переменной окружения YDB_DSN.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"time"

	"git.sourcecraft.dev/drflamov/coffeechat/server/internal/store/ydb"
)

const (
	// defaultDSN — локальная база из deploy/docker-compose.yml.
	defaultDSN = "grpc://localhost:2136/local"
	// База в контейнере стартует не мгновенно: ждём её готовности с повторами.
	readyTimeout  = 60 * time.Second
	retryInterval = 2 * time.Second
	// Срок одной попытки: подключение и запрос к ещё не готовой базе могут зависнуть.
	attemptTimeout = 10 * time.Second
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	err := run(ctx, os.Args[1:])
	stop()
	if err != nil {
		fmt.Fprintln(os.Stderr, "ошибка:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	if len(args) != 1 || args[0] != "migrate" {
		return errors.New("использование: admin migrate")
	}
	dsn := os.Getenv("YDB_DSN")
	if dsn == "" {
		dsn = defaultDSN
	}
	return migrate(ctx, dsn)
}

func migrate(ctx context.Context, dsn string) error {
	ctx, cancel := context.WithTimeout(ctx, readyTimeout)
	defer cancel()

	store, err := openWhenReady(ctx, dsn)
	if err != nil {
		return err
	}
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), attemptTimeout)
		defer cancel()
		_ = store.Close(closeCtx)
	}()

	version, err := store.Migrate(ctx)
	if err != nil {
		return err
	}
	fmt.Printf("миграции применены, версия схемы: %d\n", version)
	return nil
}

// openWhenReady подключается к базе и ждёт, пока она начнёт отвечать.
func openWhenReady(ctx context.Context, dsn string) (*ydb.Store, error) {
	for {
		store, err := tryOpen(ctx, dsn)
		if err == nil {
			return store, nil
		}
		fmt.Fprintln(os.Stderr, "база ещё не готова, повтор через", retryInterval)
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("база не ответила за %s (запущена ли она? task db:up): %w", readyTimeout, err)
		case <-time.After(retryInterval):
		}
	}
}

func tryOpen(ctx context.Context, dsn string) (*ydb.Store, error) {
	ctx, cancel := context.WithTimeout(ctx, attemptTimeout)
	defer cancel()
	store, err := ydb.Open(ctx, dsn)
	if err != nil {
		return nil, err
	}
	if err := store.Ping(ctx); err != nil {
		_ = store.Close(ctx)
		return nil, err
	}
	return store, nil
}
