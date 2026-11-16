package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeTree создаёт каталог сборки с пустыми файлами.
func writeTree(t *testing.T, files ...string) string {
	t.Helper()
	dir := t.TempDir()
	for _, name := range files {
		full := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		if err := os.WriteFile(full, []byte("x"), 0o600); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}
	return dir
}

func TestPlan(t *testing.T) {
	dir := writeTree(t, "index.html", "sw.js", "assets/index-abc.js", "assets/index-abc.css", "assets/worker-abc.js", "icons/app.png")
	uploads, err := plan(dir)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}

	want := []upload{
		{key: "assets/index-abc.css", contentType: "text/css; charset=utf-8", cacheControl: cacheImmutable},
		{key: "assets/index-abc.js", contentType: "text/javascript; charset=utf-8", cacheControl: cacheImmutable},
		{key: "assets/worker-abc.js", contentType: "text/javascript; charset=utf-8", cacheControl: cacheImmutable},
		{key: "icons/app.png", contentType: "image/png", cacheControl: cacheRevalidate},
		{key: "sw.js", contentType: "text/javascript; charset=utf-8", cacheControl: cacheRevalidate},
		// Страница — последней.
		{key: "index.html", contentType: "text/html; charset=utf-8", cacheControl: cacheRevalidate},
	}
	if len(uploads) != len(want) {
		t.Fatalf("файлов %d, ожидалось %d: %+v", len(uploads), len(want), uploads)
	}
	for i, w := range want {
		got := uploads[i]
		if got.key != w.key || got.contentType != w.contentType || got.cacheControl != w.cacheControl {
			t.Errorf("файл %d: %+v, ожидался %+v", i, got, w)
		}
		if strings.Contains(got.key, `\`) {
			t.Errorf("в имени объекта обратная косая черта: %q", got.key)
		}
		if got.file != filepath.Join(dir, filepath.FromSlash(w.key)) {
			t.Errorf("путь на диске %q", got.file)
		}
	}
}

func TestPlanRejectsUnknownExtension(t *testing.T) {
	dir := writeTree(t, "index.html", "setup.exe")
	if _, err := plan(dir); err == nil || !strings.Contains(err.Error(), "setup.exe") {
		t.Fatalf("ошибка %v, ожидалась ошибка про setup.exe", err)
	}
}

func TestPlanRejectsEmptyOrMissingDir(t *testing.T) {
	if _, err := plan(t.TempDir()); err == nil {
		t.Error("пустой каталог принят")
	}
	if _, err := plan(filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Error("несуществующий каталог принят")
	}
}

func TestRunCallsYC(t *testing.T) {
	dir := writeTree(t, "index.html", "assets/index-abc.js")
	var calls [][]string
	err := run(context.Background(), dir, "my-bucket", func(_ context.Context, args []string) error {
		calls = append(calls, args)
		return nil
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(calls) != 2 {
		t.Fatalf("вызовов yc %d, ожидалось 2", len(calls))
	}
	first := strings.Join(calls[0], " ")
	for _, part := range []string{
		"storage s3 cp ",
		" s3://my-bucket/assets/index-abc.js ",
		"--content-type text/javascript; charset=utf-8",
		"--cache-control " + cacheImmutable,
		"--no-guess-mime-type",
	} {
		if !strings.Contains(first, part) {
			t.Errorf("в команде нет %q: %s", part, first)
		}
	}
	if !strings.Contains(strings.Join(calls[1], " "), " s3://my-bucket/index.html ") {
		t.Errorf("страница выгружена не последней: %v", calls[1])
	}
}

func TestRunStopsOnError(t *testing.T) {
	dir := writeTree(t, "index.html", "assets/index-abc.js")
	calls := 0
	err := run(context.Background(), dir, "b", func(context.Context, []string) error {
		calls++
		return errors.New("boom")
	})
	if err == nil || calls != 1 {
		t.Fatalf("ошибка %v после %d вызовов; ожидалась ошибка после первого", err, calls)
	}
}
