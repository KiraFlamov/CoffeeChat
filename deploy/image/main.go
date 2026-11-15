// Команда image собирает образ контейнера сервера без Docker: один слой
// с одним файлом — статическим бинарником /app (ADR-004). Запуск: task image.
//
// Образ записывается в файл формата docker save. Его принимают docker load и crane push.
package main

import (
	"archive/tar"
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
)

const (
	// entrypoint — путь бинарника внутри образа.
	entrypoint = "/app"
	// runAsUser — непривилегированный пользователь: серверу права root не нужны.
	runAsUser = "65532:65532"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "ошибка:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	flags := flag.NewFlagSet("image", flag.ContinueOnError)
	binary := flags.String("binary", "", "путь к статическому бинарнику сервера для linux/amd64")
	out := flags.String("out", "", "куда записать образ (файл .tar)")
	tag := flags.String("tag", "", "имя образа с тегом, например coffeechat:abc1234")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *binary == "" || *out == "" || *tag == "" {
		flags.Usage()
		return errors.New("нужны все три параметра: -binary, -out, -tag")
	}

	ref, err := name.NewTag(*tag)
	if err != nil {
		return fmt.Errorf("имя образа %q: %w", *tag, err)
	}
	img, err := build(*binary)
	if err != nil {
		return err
	}
	if err := tarball.WriteToFile(*out, ref, img); err != nil {
		return fmt.Errorf("запись образа: %w", err)
	}

	digest, err := img.Digest()
	if err != nil {
		return fmt.Errorf("хэш образа: %w", err)
	}
	size, err := compressedSize(img)
	if err != nil {
		return err
	}
	fmt.Printf("образ %s записан в %s\nхэш: %s\nразмер в реестре: %.1f МБ\n",
		*tag, *out, digest, float64(size)/(1<<20))
	return nil
}

// build собирает образ из одного слоя с бинарником.
func build(binaryPath string) (v1.Image, error) {
	layerTar, err := layer(binaryPath)
	if err != nil {
		return nil, err
	}
	l, err := tarball.LayerFromOpener(func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(layerTar)), nil
	})
	if err != nil {
		return nil, fmt.Errorf("слой образа: %w", err)
	}
	img, err := mutate.AppendLayers(empty.Image, l)
	if err != nil {
		return nil, fmt.Errorf("добавление слоя: %w", err)
	}
	config, err := img.ConfigFile()
	if err != nil {
		return nil, fmt.Errorf("конфигурация образа: %w", err)
	}
	config = config.DeepCopy()
	config.Architecture = "amd64"
	config.OS = "linux"
	config.Config.Entrypoint = []string{entrypoint}
	config.Config.User = runAsUser
	// Время создания не пишем: одинаковый бинарник даёт одинаковый хэш образа.
	config.Created = v1.Time{}
	img, err = mutate.ConfigFile(img, config)
	if err != nil {
		return nil, fmt.Errorf("конфигурация образа: %w", err)
	}
	return img, nil
}

// layer упаковывает бинарник в tar-архив слоя. Права и владелец задаются явно:
// на Windows у файла нет признака «исполняемый», а в образе он обязателен.
func layer(binaryPath string) ([]byte, error) {
	data, err := os.ReadFile(binaryPath) //nolint:gosec // путь задаёт тот, кто запускает сборку
	if err != nil {
		return nil, fmt.Errorf("бинарник сервера (собран ли он? task image): %w", err)
	}
	if err := checkLinuxBinary(data); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	w := tar.NewWriter(&buf)
	header := &tar.Header{
		Name:     entrypoint[1:],
		Mode:     0o755,
		Size:     int64(len(data)),
		Typeflag: tar.TypeReg,
		ModTime:  time.Unix(0, 0),
		Format:   tar.FormatPAX,
	}
	if err := w.WriteHeader(header); err != nil {
		return nil, fmt.Errorf("архив слоя: %w", err)
	}
	if _, err := w.Write(data); err != nil {
		return nil, fmt.Errorf("архив слоя: %w", err)
	}
	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("архив слоя: %w", err)
	}
	return buf.Bytes(), nil
}

// checkLinuxBinary ловит частую ошибку: в образ попал бинарник, собранный под Windows.
func checkLinuxBinary(data []byte) error {
	if !bytes.HasPrefix(data, []byte("\x7fELF")) {
		return errors.New("бинарник не для Linux: собирай с GOOS=linux GOARCH=amd64 CGO_ENABLED=0")
	}
	return nil
}

// compressedSize — сколько образ займёт в реестре: сжатые слои и конфигурация.
func compressedSize(img v1.Image) (int64, error) {
	manifest, err := img.Manifest()
	if err != nil {
		return 0, fmt.Errorf("манифест образа: %w", err)
	}
	total := manifest.Config.Size
	for _, l := range manifest.Layers {
		total += l.Size
	}
	return total, nil
}
