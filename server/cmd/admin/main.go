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
	readyTimeout = 60 * time.Second
	// closeTimeout — срок закрытия подключения к базе.
	closeTimeout = 10 * time.Second
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

	store, err := ydb.OpenWhenReady(ctx, dsn, func() {
		fmt.Fprintln(os.Stderr, "база ещё не готова, повторяю попытку")
	})
	if err != nil {
		return fmt.Errorf("база не ответила за %s (запущена ли она? task db:up): %w", readyTimeout, err)
	}
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), closeTimeout)
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
