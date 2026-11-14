// Package storetest содержит контрактные тесты порта Store.
// Каждый адаптер (YDB, с этапа 5 — Postgres) обязан проходить один и тот же набор.
package storetest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"git.sourcecraft.dev/drflamov/coffeechat/server/internal/port"
)

// RunStoreContract проверяет адаптер Store. newStore возвращает готовое хранилище
// с применёнными миграциями. Тесты пишут строки со случайными ID и удаляют их за собой,
// поэтому набор можно запускать на непустой базе.
func RunStoreContract(t *testing.T, newStore func(t *testing.T) port.Store) {
	t.Helper()
	store := newStore(t)

	t.Run("Ping успешен на поднятой базе", func(t *testing.T) {
		if err := store.Ping(testContext(t)); err != nil {
			t.Fatalf("Ping: %v", err)
		}
	})

	t.Run("Put затем Get возвращает то же", func(t *testing.T) {
		ctx := testContext(t)
		want := newConnection(t, store, time.Hour)
		if err := store.PutConnection(ctx, want); err != nil {
			t.Fatalf("PutConnection: %v", err)
		}
		got, err := store.GetConnection(ctx, want.ID)
		if err != nil {
			t.Fatalf("GetConnection: %v", err)
		}
		assertSameConnection(t, got, want)
	})

	t.Run("повторный Put перезаписывает запись без дубля", func(t *testing.T) {
		ctx := testContext(t)
		first := newConnection(t, store, time.Hour)
		if err := store.PutConnection(ctx, first); err != nil {
			t.Fatalf("PutConnection: %v", err)
		}
		second := first
		second.ConnectedAt = first.ConnectedAt.Add(time.Minute)
		second.ExpiresAt = first.ExpiresAt.Add(time.Minute)
		if err := store.PutConnection(ctx, second); err != nil {
			t.Fatalf("повторный PutConnection: %v", err)
		}
		got, err := store.GetConnection(ctx, first.ID)
		if err != nil {
			t.Fatalf("GetConnection: %v", err)
		}
		assertSameConnection(t, got, second)
		// Дубля нет: одно удаление убирает запись целиком.
		if err := store.DeleteConnection(ctx, first.ID); err != nil {
			t.Fatalf("DeleteConnection: %v", err)
		}
		if _, err := store.GetConnection(ctx, first.ID); !errors.Is(err, port.ErrNotFound) {
			t.Fatalf("после удаления err = %v, ожидалась ErrNotFound", err)
		}
	})

	t.Run("Get несуществующего ID возвращает ErrNotFound", func(t *testing.T) {
		_, err := store.GetConnection(testContext(t), randomID(t))
		if !errors.Is(err, port.ErrNotFound) {
			t.Fatalf("err = %v, ожидалась ErrNotFound", err)
		}
	})

	t.Run("Get просроченной записи возвращает ErrNotFound", func(t *testing.T) {
		ctx := testContext(t)
		expired := newConnection(t, store, -time.Minute)
		if err := store.PutConnection(ctx, expired); err != nil {
			t.Fatalf("PutConnection: %v", err)
		}
		if _, err := store.GetConnection(ctx, expired.ID); !errors.Is(err, port.ErrNotFound) {
			t.Fatalf("err = %v, ожидалась ErrNotFound", err)
		}
	})

	t.Run("Delete дважды подряд без ошибки", func(t *testing.T) {
		ctx := testContext(t)
		c := newConnection(t, store, time.Hour)
		if err := store.PutConnection(ctx, c); err != nil {
			t.Fatalf("PutConnection: %v", err)
		}
		if err := store.DeleteConnection(ctx, c.ID); err != nil {
			t.Fatalf("первый DeleteConnection: %v", err)
		}
		if err := store.DeleteConnection(ctx, c.ID); err != nil {
			t.Fatalf("второй DeleteConnection: %v", err)
		}
	})

	t.Run("Delete несуществующего ID без ошибки", func(t *testing.T) {
		if err := store.DeleteConnection(testContext(t), randomID(t)); err != nil {
			t.Fatalf("DeleteConnection: %v", err)
		}
	})
}

func testContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// newConnection создаёт соединение со случайным ID, которое истекает через ttl,
// и удаляет его после теста. Время округлено до микросекунды: точнее базы не хранят.
func newConnection(t *testing.T, store port.Store, ttl time.Duration) port.Connection {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Microsecond)
	c := port.Connection{ID: randomID(t), ConnectedAt: now, ExpiresAt: now.Add(ttl)}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := store.DeleteConnection(ctx, c.ID); err != nil {
			t.Errorf("очистка после теста: %v", err)
		}
	})
	return c
}

func randomID(t *testing.T) string {
	t.Helper()
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("rand: %v", err)
	}
	return "test-" + hex.EncodeToString(b)
}

func assertSameConnection(t *testing.T, got, want port.Connection) {
	t.Helper()
	if got.ID != want.ID {
		t.Errorf("ID = %q, ожидался %q", got.ID, want.ID)
	}
	if !got.ConnectedAt.Equal(want.ConnectedAt) {
		t.Errorf("ConnectedAt = %v, ожидалось %v", got.ConnectedAt, want.ConnectedAt)
	}
	if !got.ExpiresAt.Equal(want.ExpiresAt) {
		t.Errorf("ExpiresAt = %v, ожидалось %v", got.ExpiresAt, want.ExpiresAt)
	}
}
