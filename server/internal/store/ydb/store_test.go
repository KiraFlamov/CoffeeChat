package ydb_test

import (
	"context"
	"os"
	"testing"
	"time"

	"git.sourcecraft.dev/drflamov/coffeechat/server/internal/port"
	"git.sourcecraft.dev/drflamov/coffeechat/server/internal/store/storetest"
	"git.sourcecraft.dev/drflamov/coffeechat/server/internal/store/ydb"
)

// openTestStore подключается к базе из переменной YDB_DSN и применяет миграции.
// Без переменной тесты пропускаются: им нужна запущенная база (task test:store).
func openTestStore(t *testing.T) *ydb.Store {
	t.Helper()
	dsn := os.Getenv("YDB_DSN")
	if dsn == "" {
		t.Skip("YDB_DSN не задана: тесты адаптера YDB пропущены. Запуск с базой: task test:store")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	store, err := ydb.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := store.Close(ctx); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	if _, err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return store
}

func TestStoreContract(t *testing.T) {
	storetest.RunStoreContract(t, func(t *testing.T) port.Store {
		return openTestStore(t)
	})
}

func TestMigrateIsIdempotent(t *testing.T) {
	store := openTestStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	first, err := store.Migrate(ctx)
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	second, err := store.Migrate(ctx)
	if err != nil {
		t.Fatalf("повторный Migrate: %v", err)
	}
	if first < 1 || first != second {
		t.Errorf("версии схемы: %d и %d, ожидались одинаковые и не меньше 1", first, second)
	}
}
