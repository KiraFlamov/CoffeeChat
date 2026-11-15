package main

import (
	"archive/tar"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-containerregistry/pkg/v1/tarball"
)

// fakeBinary — файл с заголовком ELF: настоящий бинарник тесту не нужен.
func fakeBinary(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "container")
	if err := os.WriteFile(path, append([]byte("\x7fELF"), make([]byte, 1024)...), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}

func TestImage(t *testing.T) {
	out := filepath.Join(t.TempDir(), "image.tar")
	if err := run([]string{"-binary", fakeBinary(t), "-out", out, "-tag", "coffeechat:test"}); err != nil {
		t.Fatalf("run: %v", err)
	}

	// Образ читается обратно тем же способом, что и при отправке в реестр.
	img, err := tarball.ImageFromPath(out, nil)
	if err != nil {
		t.Fatalf("образ не читается: %v", err)
	}
	config, err := img.ConfigFile()
	if err != nil {
		t.Fatalf("ConfigFile: %v", err)
	}
	if config.OS != "linux" || config.Architecture != "amd64" {
		t.Errorf("платформа %s/%s, ожидалась linux/amd64", config.OS, config.Architecture)
	}
	if len(config.Config.Entrypoint) != 1 || config.Config.Entrypoint[0] != "/app" {
		t.Errorf("точка входа %v, ожидалась [/app]", config.Config.Entrypoint)
	}
	if config.Config.User != runAsUser {
		t.Errorf("пользователь %q, ожидался %q", config.Config.User, runAsUser)
	}

	layers, err := img.Layers()
	if err != nil {
		t.Fatalf("Layers: %v", err)
	}
	if len(layers) != 1 {
		t.Fatalf("слоёв %d, ожидался 1", len(layers))
	}
	rc, err := layers[0].Uncompressed()
	if err != nil {
		t.Fatalf("Uncompressed: %v", err)
	}
	defer func() { _ = rc.Close() }()
	tr := tar.NewReader(rc)
	header, err := tr.Next()
	if err != nil {
		t.Fatalf("в слое нет файлов: %v", err)
	}
	if header.Name != "app" || header.Mode != 0o755 {
		t.Errorf("файл %q с правами %o, ожидался app с правами 755", header.Name, header.Mode)
	}
	if _, err := tr.Next(); !errors.Is(err, io.EOF) {
		t.Errorf("в слое больше одного файла")
	}
}

func TestImageIsReproducible(t *testing.T) {
	binary := fakeBinary(t)
	first, err := build(binary)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	second, err := build(binary)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	d1, _ := first.Digest()
	d2, _ := second.Digest()
	if d1 != d2 {
		t.Errorf("хэши двух сборок различаются: %s и %s", d1, d2)
	}
}

func TestRejectsNonLinuxBinary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "container.exe")
	if err := os.WriteFile(path, []byte("MZ windows binary"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	err := run([]string{"-binary", path, "-out", filepath.Join(t.TempDir(), "image.tar"), "-tag", "coffeechat:test"})
	if err == nil {
		t.Fatal("бинарник не для Linux принят")
	}
}

func TestRequiresAllFlags(t *testing.T) {
	if err := run([]string{"-binary", "x"}); err == nil {
		t.Fatal("запуск без -out и -tag прошёл")
	}
}
