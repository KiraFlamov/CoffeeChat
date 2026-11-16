// Команда webupload выгружает сборку фронтенда в бакет Object Storage через yc.
// Запуск: task deploy-web ENV=staging.
//
//	webupload -dir ../web/dist -bucket coffeechat-staging
//
// Файлы выгружаются по одному, с явным именем объекта и типом содержимого.
// Одной командой «yc storage s3 cp --recursive» на Windows пользоваться нельзя:
// она пишет имена с обратной косой чертой и берёт тип файла из реестра Windows
// (для .js это text/plain — браузер такой скрипт не выполняет).
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

const (
	// Файлы с хэшем в имени не меняются — браузер может хранить их долго.
	cacheImmutable = "public, max-age=31536000, immutable"
	// Страница и Service Worker проверяются заново при каждом открытии,
	// иначе обновление не дойдёт до пользователя.
	cacheRevalidate = "no-cache"
	// hashedDir — каталог сборки Vite, в котором у всех файлов хэш в имени.
	hashedDir = "assets/"
)

// contentTypes — тип содержимого по расширению. Список свой, а не системный:
// системный на Windows берётся из реестра и ошибается. Новое расширение
// в сборке — ошибка выгрузки: его нужно добавить сюда осознанно.
var contentTypes = map[string]string{
	".html":        "text/html; charset=utf-8",
	".js":          "text/javascript; charset=utf-8",
	".mjs":         "text/javascript; charset=utf-8",
	".css":         "text/css; charset=utf-8",
	".json":        "application/json",
	".map":         "application/json",
	".webmanifest": "application/manifest+json",
	".txt":         "text/plain; charset=utf-8",
	".svg":         "image/svg+xml",
	".png":         "image/png",
	".jpg":         "image/jpeg",
	".webp":        "image/webp",
	".ico":         "image/x-icon",
	".woff2":       "font/woff2",
	".wasm":        "application/wasm",
}

// upload — один файл сборки и то, как его положить в бакет.
type upload struct {
	file         string // путь на диске
	key          string // имя объекта в бакете, всегда с прямой косой чертой
	contentType  string
	cacheControl string
}

func main() {
	dir := flag.String("dir", "", "каталог сборки фронтенда (web/dist)")
	bucket := flag.String("bucket", "", "имя бакета")
	flag.Parse()
	if *dir == "" || *bucket == "" {
		flag.Usage()
		fmt.Fprintln(os.Stderr, "ошибка: нужны параметры -dir и -bucket (развёрнут ли стенд?)")
		os.Exit(2)
	}
	if err := run(context.Background(), *dir, *bucket, runYC); err != nil {
		fmt.Fprintln(os.Stderr, "ошибка:", err)
		os.Exit(1)
	}
}

// run выгружает все файлы каталога. exec выполняет одну команду yc с данными аргументами.
func run(ctx context.Context, dir, bucket string, exec func(ctx context.Context, args []string) error) error {
	uploads, err := plan(dir)
	if err != nil {
		return err
	}
	for _, u := range uploads {
		args := []string{
			"storage", "s3", "cp", u.file, "s3://" + bucket + "/" + u.key,
			"--content-type", u.contentType,
			"--cache-control", u.cacheControl,
			"--no-guess-mime-type",
			"--only-show-errors",
		}
		if err := exec(ctx, args); err != nil {
			return fmt.Errorf("выгрузка %s: %w", u.key, err)
		}
		fmt.Printf("%-40s %s\n", u.key, u.contentType)
	}
	fmt.Printf("выгружено файлов: %d\n", len(uploads))
	return nil
}

// plan обходит каталог сборки и решает, как выгружать каждый файл.
// Страница (index.html) идёт последней: к этому моменту её скрипты уже в бакете.
func plan(dir string) ([]upload, error) {
	var uploads []upload
	err := filepath.WalkDir(dir, func(file string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dir, file)
		if err != nil {
			return fmt.Errorf("путь %s: %w", file, err)
		}
		key := filepath.ToSlash(rel)
		contentType, ok := contentTypes[strings.ToLower(path.Ext(key))]
		if !ok {
			return fmt.Errorf("файл %s: неизвестное расширение, добавь его в contentTypes", key)
		}
		cacheControl := cacheRevalidate
		if strings.HasPrefix(key, hashedDir) {
			cacheControl = cacheImmutable
		}
		uploads = append(uploads, upload{file: file, key: key, contentType: contentType, cacheControl: cacheControl})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("каталог сборки (собран ли фронтенд?): %w", err)
	}
	if len(uploads) == 0 {
		return nil, errors.New("каталог сборки пуст")
	}
	sort.SliceStable(uploads, func(i, j int) bool {
		iPage, jPage := uploads[i].key == "index.html", uploads[j].key == "index.html"
		if iPage != jPage {
			return jPage
		}
		return uploads[i].key < uploads[j].key
	})
	return uploads, nil
}

// runYC запускает yc. Аргументы собираются из имён файлов сборки и имени бакета,
// через оболочку они не проходят.
func runYC(ctx context.Context, args []string) error {
	cmd := exec.CommandContext(ctx, "yc", args...) //nolint:gosec // G204: см. комментарий выше
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("yc: %w", err)
	}
	return nil
}
