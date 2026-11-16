// Команда admin — служебные операции над стендом. Пока одна: применить миграции базы.
//
//	admin migrate
//
// Строка подключения берётся из переменной окружения YDB_DSN.
// Для базы в облаке нужна ещё YDB_ACCESS_TOKEN — IAM-токен (yc iam create-token);
// локальной базе токен не нужен. YDB_DEBUG=1 включает подробный журнал драйвера базы.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"time"

	sdk "github.com/ydb-platform/ydb-go-sdk/v3"
	ydblog "github.com/ydb-platform/ydb-go-sdk/v3/log"
	"github.com/ydb-platform/ydb-go-sdk/v3/trace"
	ycmeta "github.com/ydb-platform/ydb-go-yc-metadata"

	"git.sourcecraft.dev/drflamov/coffeechat/server/internal/store/ydb"
)

const (
	// defaultDSN — локальная база из deploy/docker-compose.yml.
	defaultDSN = "grpc://localhost:2136/local"
	// База в контейнере стартует не мгновенно: ждём её готовности с повторами.
	readyTimeout = 60 * time.Second
	// migrateTimeout — срок самих миграций: создание таблиц в облаке занимает секунды.
	migrateTimeout = 2 * time.Minute
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
	opts := cloudOptions(os.Getenv("YDB_ACCESS_TOKEN"))
	if os.Getenv("YDB_DEBUG") != "" {
		opts = append(opts, debugLog())
	}
	return migrate(ctx, dsn, opts)
}

// debugLog включает журнал драйвера базы в stderr: подключения, сессии, запросы и повторы.
// События входа (токены) в журнал не попадают.
func debugLog() sdk.Option {
	details := trace.DatabaseSQLEvents | trace.QueryEvents | trace.TableEvents | trace.ScriptingEvents |
		trace.RetryEvents | trace.DiscoveryEvents | trace.DriverConnEvents | trace.DriverBalancerEvents
	return sdk.WithLogger(ydblog.Default(os.Stderr, ydblog.WithMinLevel(ydblog.DEBUG)), details)
}

// cloudOptions — настройки подключения к базе в облаке: вход по IAM-токену
// и сертификат Yandex Cloud. Без токена (локальная база) настроек нет.
func cloudOptions(token string) []sdk.Option {
	if token == "" {
		return nil
	}
	return []sdk.Option{sdk.WithAccessTokenCredentials(token), ycmeta.WithInternalCA()}
}

func migrate(ctx context.Context, dsn string, opts []sdk.Option) error {
	readyCtx, cancelReady := context.WithTimeout(ctx, readyTimeout)
	defer cancelReady()

	store, err := ydb.OpenWhenReady(readyCtx, dsn, func() {
		fmt.Fprintln(os.Stderr, "база ещё не готова, повторяю попытку")
	}, opts...)
	if err != nil {
		return fmt.Errorf("база не ответила за %s (запущена ли она? task db:up): %w", readyTimeout, err)
	}
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), closeTimeout)
		defer cancel()
		_ = store.Close(closeCtx)
	}()

	// У миграций свой срок: время ожидания базы в него не входит.
	migrateCtx, cancelMigrate := context.WithTimeout(ctx, migrateTimeout)
	defer cancelMigrate()
	version, err := store.Migrate(migrateCtx)
	if err != nil {
		return err
	}
	fmt.Printf("миграции применены, версия схемы: %d\n", version)
	return nil
}
