package ydb

import (
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
)

func TestSplitStatements(t *testing.T) {
	script := "-- комментарий; не запрос\r\n" +
		"CREATE TABLE a (\r\n    id Utf8, -- поле; с точкой с запятой\r\n    PRIMARY KEY (id)\r\n);\r\n" +
		"\r\n-- только комментарий\r\n" +
		"ALTER TABLE a ADD COLUMN b Utf8;\n" +
		"CREATE TABLE c (id Utf8, PRIMARY KEY (id))" // без точки с запятой в конце файла
	got := splitStatements(script)
	if len(got) != 3 {
		t.Fatalf("запросов %d, ожидалось 3: %q", len(got), got)
	}
	for i, prefix := range []string{"-- комментарий; не запрос\nCREATE TABLE a", "-- только комментарий\nALTER TABLE a", "CREATE TABLE c"} {
		if !strings.HasPrefix(got[i], prefix) {
			t.Errorf("запрос %d: %q, ожидалось начало %q", i+1, got[i], prefix)
		}
	}
	if strings.Contains(got[0], "\r") {
		t.Errorf("в запросе остался символ возврата каретки: %q", got[0])
	}
}

func TestLoadMigrationsOrder(t *testing.T) {
	fsys := fstest.MapFS{
		"m/0010_c.sql":   {Data: []byte("CREATE TABLE c (id Utf8, PRIMARY KEY (id));")},
		"m/0002_b.sql":   {Data: []byte("CREATE TABLE b (id Utf8, PRIMARY KEY (id));")},
		"m/0001_a.sql":   {Data: []byte("CREATE TABLE a (id Utf8, PRIMARY KEY (id));")},
		"m/README.md":    {Data: []byte("не миграция")},
		"m/sub/x.sql":    {Data: []byte("SELECT 1;")},
		"other/0003.sql": {Data: []byte("SELECT 1;")},
	}
	migrations, err := loadMigrations(fsys, "m")
	if err != nil {
		t.Fatalf("loadMigrations: %v", err)
	}
	var versions []uint64
	for _, m := range migrations {
		versions = append(versions, m.version)
	}
	if !reflect.DeepEqual(versions, []uint64{1, 2, 10}) {
		t.Fatalf("порядок %v, ожидался [1 2 10]", versions)
	}
}

func TestLoadMigrationsRejectsBadFiles(t *testing.T) {
	cases := map[string]fstest.MapFS{
		"нет номера":       {"m/connections.sql": {Data: []byte("SELECT 1;")}},
		"номер не число":   {"m/abc_x.sql": {Data: []byte("SELECT 1;")}},
		"нулевой номер":    {"m/0000_x.sql": {Data: []byte("SELECT 1;")}},
		"одинаковый номер": {"m/0001_a.sql": {Data: []byte("SELECT 1;")}, "m/1_b.sql": {Data: []byte("SELECT 1;")}},
		"пустая миграция":  {"m/0001_a.sql": {Data: []byte("-- только комментарий\n")}},
	}
	for name, fsys := range cases {
		if _, err := loadMigrations(fsys, "m"); err == nil {
			t.Errorf("%s: ошибки нет", name)
		}
	}
}

func TestEmbeddedMigrationsAreValid(t *testing.T) {
	migrations, err := loadMigrations(migrationsFS, "migrations")
	if err != nil {
		t.Fatalf("loadMigrations: %v", err)
	}
	if len(migrations) == 0 {
		t.Fatal("миграций нет")
	}
}
