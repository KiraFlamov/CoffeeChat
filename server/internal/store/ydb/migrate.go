package ydb

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"

	"github.com/pressly/goose/v3"
	sdk "github.com/ydb-platform/ydb-go-sdk/v3"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrate применяет все ещё не применённые миграции и возвращает номер версии схемы.
// Повторный вызов ничего не меняет. Номера применённых миграций goose хранит
// в служебной таблице goose_db_version.
func (s *Store) Migrate(ctx context.Context) (version int64, err error) {
	// goose работает через database/sql. Режим scripting нужен, чтобы в одном
	// соединении выполнялись и DDL (CREATE TABLE), и запросы к служебной таблице;
	// настоящих транзакций у DDL в YDB нет, поэтому они подменяются «пустыми».
	// Настройки взяты из документации goose для YDB (ADR-003).
	connector, err := sdk.Connector(s.driver,
		sdk.WithDefaultQueryMode(sdk.ScriptingQueryMode),
		sdk.WithFakeTx(sdk.ScriptingQueryMode),
		sdk.WithAutoDeclare(),
		sdk.WithNumericArgs(),
	)
	if err != nil {
		return 0, fmt.Errorf("ydb sql connector: %w", err)
	}
	db := sql.OpenDB(connector)
	defer func() {
		if closeErr := db.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("close migrations connection: %w", closeErr)
		}
	}()

	files, err := fs.Sub(migrationsFS, "migrations")
	if err != nil {
		return 0, fmt.Errorf("migrations fs: %w", err)
	}
	provider, err := goose.NewProvider(goose.DialectYdB, db, files)
	if err != nil {
		return 0, fmt.Errorf("goose provider: %w", err)
	}
	if _, err := provider.Up(ctx); err != nil {
		return 0, fmt.Errorf("apply migrations: %w", err)
	}
	version, err = provider.GetDBVersion(ctx)
	if err != nil {
		return 0, fmt.Errorf("read schema version: %w", err)
	}
	return version, nil
}
