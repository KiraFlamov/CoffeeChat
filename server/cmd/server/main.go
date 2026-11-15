// Команда server — локальный сервер разработки: тот же обработчик ConnectRPC,
// что и в облаке, и настоящий WebSocket на /ws. Запуск: task dev.
//
// Переменные окружения (у всех есть значения по умолчанию для локального стенда):
//
//	ADDR         адрес, на котором слушает сервер (localhost:8080)
//	YDB_DSN      строка подключения к базе (grpc://localhost:2136/local)
//	CORS_ORIGIN  origin фронтенда, которому разрешён доступ (http://localhost:5173)
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"time"

	"git.sourcecraft.dev/drflamov/coffeechat/server/internal/core"
	"git.sourcecraft.dev/drflamov/coffeechat/server/internal/store/ydb"
	"git.sourcecraft.dev/drflamov/coffeechat/server/internal/transport/hub"
	"git.sourcecraft.dev/drflamov/coffeechat/server/internal/version"
)

const (
	// Слушаем только localhost: сервер разработки не должен быть виден из сети.
	defaultAddr = "localhost:8080"
	// defaultDSN — локальная база из deploy/docker-compose.yml.
	defaultDSN = "grpc://localhost:2136/local"
	// defaultOrigin — адрес Vite.
	defaultOrigin = "http://localhost:5173"

	// База в контейнере стартует не мгновенно: ждём её готовности с повторами.
	readyTimeout    = 60 * time.Second
	shutdownTimeout = 5 * time.Second
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	err := run(ctx)
	stop()
	if err != nil {
		fmt.Fprintln(os.Stderr, "ошибка:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	addr := envOr("ADDR", defaultAddr)
	dsn := envOr("YDB_DSN", defaultDSN)
	origin, err := parseOrigin(envOr("CORS_ORIGIN", defaultOrigin))
	if err != nil {
		return err
	}

	store, err := openStore(ctx, dsn)
	if err != nil {
		return err
	}
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		_ = store.Close(closeCtx)
	}()

	connections := hub.New()
	system := core.NewSystem(store, connections, version.String(), time.Now)

	srv := &http.Server{
		Addr:              addr,
		Handler:           newHandler(system, connections, origin),
		ReadHeaderTimeout: 10 * time.Second,
		// Контекст запросов отменяется по Ctrl+C: так закрываются открытые WebSocket,
		// которых Shutdown не ждёт.
		BaseContext: func(net.Listener) context.Context { return ctx },
	}

	// Порт занимаем до сообщения о запуске: иначе при занятом порте журнал врёт.
	var lc net.ListenConfig
	listener, err := lc.Listen(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("сервер не запустился на %s (порт занят? другой порт: переменная ADDR): %w", addr, err)
	}
	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(listener) }()
	slog.Info("server started", "addr", addr, "origin", origin.String(), "version", version.String())

	select {
	case err := <-serveErr:
		return fmt.Errorf("сервер остановился: %w", err)
	case <-ctx.Done():
	}

	slog.Info("server stopping")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("остановка сервера: %w", err)
	}
	return nil
}

func openStore(ctx context.Context, dsn string) (*ydb.Store, error) {
	ctx, cancel := context.WithTimeout(ctx, readyTimeout)
	defer cancel()
	store, err := ydb.OpenWhenReady(ctx, dsn, func() {
		slog.Info("database is not ready, retrying")
	})
	if err != nil {
		return nil, fmt.Errorf("база не ответила за %s (запущена ли она? task db:up): %w", readyTimeout, err)
	}
	return store, nil
}

// parseOrigin проверяет, что origin имеет вид схема://хост[:порт] без пути.
func parseOrigin(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" ||
		u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return nil, fmt.Errorf("CORS_ORIGIN %q: нужен адрес вида http://localhost:5173", raw)
	}
	return u, nil
}

func envOr(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}
