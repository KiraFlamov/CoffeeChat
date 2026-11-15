package hub_test

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"

	"git.sourcecraft.dev/drflamov/coffeechat/server/internal/port"
	"git.sourcecraft.dev/drflamov/coffeechat/server/internal/transport/hub"
)

func TestSendDeliversToRegisteredConnection(t *testing.T) {
	h := hub.New()
	var got []byte
	h.Register("c1", func(_ context.Context, data []byte) error {
		got = data
		return nil
	})

	if err := h.Send(context.Background(), "c1", []byte{1, 2, 3}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if !bytes.Equal(got, []byte{1, 2, 3}) {
		t.Fatalf("доставлено %v, ожидалось [1 2 3]", got)
	}
}

func TestSendToUnknownConnectionIsGone(t *testing.T) {
	h := hub.New()
	if err := h.Send(context.Background(), "nope", []byte{1}); !errors.Is(err, port.ErrGone) {
		t.Fatalf("ошибка %v, ожидалась ErrGone", err)
	}
}

func TestSendAfterUnregisterIsGone(t *testing.T) {
	h := hub.New()
	h.Register("c1", func(context.Context, []byte) error { return nil })
	h.Unregister("c1")
	h.Unregister("c1") // повтор — не ошибка

	if err := h.Send(context.Background(), "c1", []byte{1}); !errors.Is(err, port.ErrGone) {
		t.Fatalf("ошибка %v, ожидалась ErrGone", err)
	}
}

func TestSendToBrokenConnectionIsGone(t *testing.T) {
	h := hub.New()
	h.Register("c1", func(context.Context, []byte) error { return errors.New("broken pipe") })

	if err := h.Send(context.Background(), "c1", []byte{1}); !errors.Is(err, port.ErrGone) {
		t.Fatalf("ошибка %v, ожидалась ErrGone", err)
	}
}

func TestSendWithExpiredContextIsNotGone(t *testing.T) {
	h := hub.New()
	h.Register("c1", func(ctx context.Context, _ []byte) error { return ctx.Err() })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := h.Send(ctx, "c1", []byte{1})
	if err == nil || errors.Is(err, port.ErrGone) {
		t.Fatalf("ошибка %v, ожидалась ошибка контекста, а не ErrGone", err)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("ошибка %v не оборачивает context.Canceled", err)
	}
}

// Проверка для детектора гонок: регистрация, отправка и удаление идут одновременно.
func TestConcurrentUse(t *testing.T) {
	h := hub.New()
	var wg sync.WaitGroup
	for i := range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id := string(rune('a' + i%5))
			h.Register(id, func(context.Context, []byte) error { return nil })
			_ = h.Send(context.Background(), id, []byte{1})
			h.Unregister(id)
		}()
	}
	wg.Wait()
}
