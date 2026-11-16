package ydb

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
	"time"

	sdk "github.com/ydb-platform/ydb-go-sdk/v3"
	"github.com/ydb-platform/ydb-go-sdk/v3/query"
	"github.com/ydb-platform/ydb-go-sdk/v3/table"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// statementTimeout — срок одного запроса миграции. Без него зависший запрос
// молча съедает всё время и не видно, какой именно.
const statementTimeout = time.Minute

// Таблица с номерами применённых миграций.
const createMigrationsTable = `
CREATE TABLE IF NOT EXISTS schema_migrations (
    version    Uint64 NOT NULL,    -- номер миграции из имени файла
    name       Utf8 NOT NULL,      -- имя файла
    applied_at Timestamp NOT NULL,
    PRIMARY KEY (version)
);`

// migration — один файл из каталога migrations: NNNN_имя.sql.
type migration struct {
	version    uint64
	name       string
	statements []string
}

// Migrate применяет все ещё не применённые миграции по порядку номеров и возвращает
// номер версии схемы. Повторный вызов ничего не меняет.
//
// Это свой минимальный применятель (ADR-003): файлы по порядку, номера применённых —
// в таблице schema_migrations. Запускать его одновременно из двух мест нельзя.
//
// В файлах миграций — только запросы к схеме (CREATE, ALTER, DROP). Они выполняются
// через сервис Table одним вызовом ExecuteSchemeQuery.
func (s *Store) Migrate(ctx context.Context) (int64, error) {
	migrations, err := loadMigrations(migrationsFS, "migrations")
	if err != nil {
		return 0, err
	}
	if err := s.execScheme(ctx, createMigrationsTable); err != nil {
		return 0, fmt.Errorf("create schema_migrations: %w", err)
	}
	current, err := s.schemaVersion(ctx)
	if err != nil {
		return 0, err
	}
	for _, m := range migrations {
		if m.version <= current {
			continue
		}
		for i, statement := range m.statements {
			if err := s.execScheme(ctx, statement); err != nil {
				return 0, fmt.Errorf("migration %s, statement %d: %w", m.name, i+1, err)
			}
		}
		if err := s.recordMigration(ctx, m); err != nil {
			return 0, err
		}
		current = m.version
	}
	return int64(current), nil //nolint:gosec // номер миграции — небольшое число
}

// execScheme выполняет запрос к схеме. Запросы миграций идемпотентны (IF NOT EXISTS),
// поэтому при временной ошибке драйвер повторяет их сам.
func (s *Store) execScheme(ctx context.Context, statement string) error {
	ctx, cancel := context.WithTimeout(ctx, statementTimeout)
	defer cancel()
	return s.driver.Table().Do(ctx, func(ctx context.Context, session table.Session) error {
		return session.ExecuteSchemeQuery(ctx, statement)
	}, table.WithIdempotent())
}

func (s *Store) exec(ctx context.Context, statement string, opts ...query.ExecuteOption) error {
	ctx, cancel := context.WithTimeout(ctx, statementTimeout)
	defer cancel()
	return s.driver.Query().Exec(ctx, statement, opts...)
}

// schemaVersion возвращает номер последней применённой миграции или 0.
func (s *Store) schemaVersion(ctx context.Context) (uint64, error) {
	ctx, cancel := context.WithTimeout(ctx, statementTimeout)
	defer cancel()
	row, err := s.driver.Query().QueryRow(ctx,
		`SELECT COALESCE(MAX(version), 0ul) FROM schema_migrations;`,
		query.WithIdempotent(),
	)
	if err != nil {
		return 0, fmt.Errorf("read schema version: %w", err)
	}
	var version uint64
	if err := row.Scan(&version); err != nil {
		return 0, fmt.Errorf("scan schema version: %w", err)
	}
	return version, nil
}

func (s *Store) recordMigration(ctx context.Context, m migration) error {
	err := s.exec(ctx, `
DECLARE $version AS Uint64;
DECLARE $name AS Utf8;
UPSERT INTO schema_migrations (version, name, applied_at)
VALUES ($version, $name, CurrentUtcTimestamp());`,
		query.WithParameters(sdk.ParamsBuilder().
			Param("$version").Uint64(m.version).
			Param("$name").Text(m.name).
			Build()),
		query.WithIdempotent(),
	)
	if err != nil {
		return fmt.Errorf("record migration %s: %w", m.name, err)
	}
	return nil
}

// loadMigrations читает файлы миграций и сортирует их по номеру.
func loadMigrations(fsys fs.FS, dir string) ([]migration, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, fmt.Errorf("read migrations: %w", err)
	}
	var migrations []migration
	seen := map[uint64]string{}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".sql") {
			continue
		}
		prefix, _, ok := strings.Cut(name, "_")
		if !ok {
			return nil, fmt.Errorf("migration %s: name must look like 0001_name.sql", name)
		}
		version, err := strconv.ParseUint(prefix, 10, 64)
		if err != nil || version == 0 {
			return nil, fmt.Errorf("migration %s: name must start with a positive number", name)
		}
		if other, dup := seen[version]; dup {
			return nil, fmt.Errorf("migrations %s and %s have the same number", other, name)
		}
		seen[version] = name
		data, err := fs.ReadFile(fsys, dir+"/"+name)
		if err != nil {
			return nil, fmt.Errorf("read migration %s: %w", name, err)
		}
		statements := splitStatements(string(data))
		if len(statements) == 0 {
			return nil, fmt.Errorf("migration %s is empty", name)
		}
		migrations = append(migrations, migration{version: version, name: name, statements: statements})
	}
	sort.Slice(migrations, func(i, j int) bool { return migrations[i].version < migrations[j].version })
	return migrations, nil
}

// splitStatements делит файл на запросы. Запрос заканчивается точкой с запятой
// в конце строки. Куски из одних комментариев и пустых строк отбрасываются.
func splitStatements(script string) []string {
	script = strings.ReplaceAll(script, "\r\n", "\n")
	var (
		statements []string
		current    strings.Builder
	)
	flush := func() {
		statement := strings.TrimSpace(current.String())
		current.Reset()
		if hasSQL(statement) {
			statements = append(statements, statement)
		}
	}
	for _, line := range strings.Split(script, "\n") {
		current.WriteString(line)
		current.WriteString("\n")
		if strings.HasSuffix(strings.TrimSpace(line), ";") {
			flush()
		}
	}
	flush()
	return statements
}

// hasSQL сообщает, есть ли в тексте что-то кроме комментариев и пустых строк.
func hasSQL(text string) bool {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "--") {
			return true
		}
	}
	return false
}
