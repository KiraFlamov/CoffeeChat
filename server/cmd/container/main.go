// Команда container — сервер для Yandex Serverless Containers. Стоит за API Gateway:
// шлюз присылает и запросы HTTP API, и события WebSocket обычными HTTP-запросами.
//
// Переменные окружения:
//
//	PORT     порт, на котором слушает сервер; задаёт платформа (по умолчанию 8080)
//	YDB_DSN  строка подключения к базе, обязательна:
//	         grpcs://ydb.serverless.yandexcloud.net:2135/<путь базы>
//
// В базу и в Send API сервер ходит от имени сервисного аккаунта контейнера:
// IAM-токен выдаёт сервис метаданных, ключей в окружении нет.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	ycmeta "github.com/ydb-platform/ydb-go-yc-metadata"
	// В образе нет файлов операционной системы, а значит и корневых сертификатов:
	// набор сертификатов встраивается в бинарник (ADR-004).
	_ "golang.org/x/crypto/x509roots/fallback"

	"git.sourcecraft.dev/drflamov/coffeechat/server/internal/core"
	"git.sourcecraft.dev/drflamov/coffeechat/server/internal/store/ydb"
	"git.sourcecraft.dev/drflamov/coffeechat/server/internal/transport/yc"
	"git.sourcecraft.dev/drflamov/coffeechat/server/internal/version"
)

const (
	defaultPort = "8080"
	// openTimeout — срок подключения к базе при старте экземпляра.
	openTimeout = 8 * time.Second
	// shutdownTimeout — сколько ждём завершения запросов при остановке экземпляра.
	shutdownTimeout = 5 * time.Second
)

func main() {
	// Журнал в JSON: так его разбирает Cloud Logging.
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := run(ctx)
	stop()
	if err != nil {
		slog.Error("server failed", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	port := os.Getenv("PORT")
	if port == "" {
		port = defaultPort
	}
	dsn := os.Getenv("YDB_DSN")
	if dsn == "" {
		return errors.New("YDB_DSN is not set")
	}

	// Подключение к базе создаётся один раз на экземпляр контейнера.
	openCtx, cancel := context.WithTimeout(ctx, openTimeout)
	store, err := ydb.Open(openCtx, dsn, ycmeta.WithCredentials(), ycmeta.WithInternalCA())
	cancel()
	if err != nil {
		return err
	}
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		_ = store.Close(closeCtx)
	}()

	system := core.NewSystem(store, yc.New(), version.String(), time.Now)

	var lc net.ListenConfig
	listener, err := lc.Listen(ctx, "tcp", ":"+port)
	if err != nil {
		return fmt.Errorf("listen on port %s: %w", port, err)
	}
	srv := &http.Server{
		Handler:           newHandler(system),
		ReadHeaderTimeout: 10 * time.Second,
	}
	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(listener) }()
	slog.Info("server started", "port", port, "version", version.String())

	select {
	case err := <-serveErr:
		return fmt.Errorf("serve: %w", err)
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	return nil
}
