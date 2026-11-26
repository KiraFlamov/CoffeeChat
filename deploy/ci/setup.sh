#!/usr/bin/env bash
# Установка инструментов на воркер CI SourceCraft (Linux, amd64).
# Запуск: deploy/ci/setup.sh go node task golangci-lint gitleaks terraform yc
#
# Версии и контрольные суммы зафиксированы здесь. Суммы сверены с опубликованными
# файлами контрольных сумм проектов 17.11.2026. Новая версия — новая сумма:
# sha256sum скачанного архива сверить с официальным списком сумм.
#
# Кэша между запусками в CI SourceCraft нет (документация не описывает),
# поэтому всё скачивается каждый раз.
set -euo pipefail

GO_VERSION=1.27.4
GO_SHA256=7226736c2339719cda28fbf3a54d71aa63bcefe376e98cef00034268dab24840
NODE_VERSION=26.13.1
NODE_SHA256=5c063e46bab0fbc1c17cc445d7ff129cbdcd0471be6ed83b6ab864f8143bc864
# Та же версия pnpm, что у автора: от неё зависит формат pnpm-lock.yaml.
PNPM_VERSION=12.18.0
TASK_VERSION=3.54.0
TASK_SHA256=680859dbb4d881a9c72d4d9a8f510825450849af8567deacd7302c01124416fb
GOLANGCI_VERSION=2.15.0
GOLANGCI_SHA256=9b7fe6a6bf6334b98decf8d86b11415ea7889afdabdd1959fdce23af2a9ebcea
GITLEAKS_VERSION=8.30.1
GITLEAKS_SHA256=551f6fc83ea457d62a0d98237cbad105af8d557003051f41f3e7ca7b3f2470eb
# Terraform — с зеркала Яндекса: сайт HashiCorp из России закрыт.
TERRAFORM_VERSION=1.16.0
TERRAFORM_SHA256=41d05b927aa174f15d1228c4eba832a323b716c68b415ef5a00179f46cc602d3
YC_VERSION=1.46.0
YC_SHA256=f6fdd8633e055ae809b254c7bfe3eff76d058604cc566c5e9da15c725d74f401

# Каталог установки. CI_PREFIX нужен только для проверки скрипта без прав root.
PREFIX=${CI_PREFIX:-/usr/local}
BIN=$PREFIX/bin
SUDO=""
if [ ! -w "$PREFIX" ]; then SUDO="sudo"; fi
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

# fetch "URL1 URL2 ..." SHA256 FILE — скачать с первого источника, который отдаёт файл
# быстро и с верной контрольной суммой. Зеркала безопасны: файл сверяется с суммой
# из официального списка, подменённый не пройдёт.
# Медленная загрузка обрывается (меньше 300 КБ/с дольше 20 с) — пробуем следующий источник.
fetch() {
  local url t
  for url in $1; do
    t=$SECONDS
    if curl -fsSL --connect-timeout 15 --speed-limit 300000 --speed-time 20 -o "$TMP/$3" "$url" &&
      echo "$2  $TMP/$3" | sha256sum -c --quiet - 2>/dev/null; then
      echo "скачано $3 за $((SECONDS - t)) с: $url"
      return 0
    fi
    echo "не вышло за $((SECONDS - t)) с: $url — пробую следующий источник"
    rm -f "$TMP/$3"
  done
  echo "не удалось скачать $3 ни из одного источника" >&2
  return 1
}

install_go() {
  # Сервер go.dev (Google) из Yandex Cloud отдаёт файл очень медленно (CI 26.11.2026),
  # поэтому сначала зеркало.
  fetch "https://mirrors.aliyun.com/golang/go${GO_VERSION}.linux-amd64.tar.gz
    https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz" "$GO_SHA256" go.tgz
  $SUDO rm -rf "$PREFIX/go"
  $SUDO tar -C "$PREFIX" -xzf "$TMP/go.tgz"
  $SUDO ln -sf "$PREFIX/go/bin/go" "$PREFIX/go/bin/gofmt" "$BIN/"
  # Компилятор C нужен детектору гонок (go test -race).
  if ! command -v gcc >/dev/null; then
    echo "gcc нет — ставлю через apt"
    $SUDO apt-get update -qq && $SUDO apt-get install -y -qq gcc
  fi
}

install_node() {
  fetch "https://nodejs.org/dist/v${NODE_VERSION}/node-v${NODE_VERSION}-linux-x64.tar.xz
    https://cdn.npmmirror.com/binaries/node/v${NODE_VERSION}/node-v${NODE_VERSION}-linux-x64.tar.xz" "$NODE_SHA256" node.txz
  $SUDO tar -C "$PREFIX" --strip-components=1 -xJf "$TMP/node.txz" \
    "node-v${NODE_VERSION}-linux-x64/bin" "node-v${NODE_VERSION}-linux-x64/lib"
  $SUDO npm install -g --silent "pnpm@${PNPM_VERSION}"
}

install_task() {
  fetch "https://github.com/go-task/task/releases/download/v${TASK_VERSION}/task_linux_amd64.tar.gz" "$TASK_SHA256" task.tgz
  $SUDO tar -C "$BIN" -xzf "$TMP/task.tgz" task
}

install_golangci_lint() {
  fetch "https://github.com/golangci/golangci-lint/releases/download/v${GOLANGCI_VERSION}/golangci-lint-${GOLANGCI_VERSION}-linux-amd64.tar.gz" "$GOLANGCI_SHA256" gl.tgz
  $SUDO tar -C "$BIN" --strip-components=1 -xzf "$TMP/gl.tgz" "golangci-lint-${GOLANGCI_VERSION}-linux-amd64/golangci-lint"
}

install_gitleaks() {
  fetch "https://github.com/gitleaks/gitleaks/releases/download/v${GITLEAKS_VERSION}/gitleaks_${GITLEAKS_VERSION}_linux_x64.tar.gz" "$GITLEAKS_SHA256" gitleaks.tgz
  $SUDO tar -C "$BIN" -xzf "$TMP/gitleaks.tgz" gitleaks
}

install_terraform() {
  fetch "https://hashicorp-releases.yandexcloud.net/terraform/${TERRAFORM_VERSION}/terraform_${TERRAFORM_VERSION}_linux_amd64.zip" "$TERRAFORM_SHA256" tf.zip
  # unzip может не быть на воркере; python3 тоже не гарантирован — пробуем оба.
  if command -v unzip >/dev/null; then
    $SUDO unzip -oq "$TMP/tf.zip" terraform -d "$BIN"
  else
    python3 -c "import zipfile,sys; zipfile.ZipFile(sys.argv[1]).extract('terraform', sys.argv[2])" "$TMP/tf.zip" "$TMP"
    $SUDO install -m 0755 "$TMP/terraform" "$BIN/terraform"
  fi
  # Провайдер — тоже с зеркала Яндекса (как на ПК, deploy/README.md).
  cat >"$HOME/.terraformrc" <<'EOF'
provider_installation {
  network_mirror {
    url     = "https://terraform-mirror.yandexcloud.net/"
    include = ["registry.terraform.io/*/*"]
  }
  direct {
    exclude = ["registry.terraform.io/*/*"]
  }
}
EOF
}

install_yc() {
  fetch "https://storage.yandexcloud.net/yandexcloud-yc/release/${YC_VERSION}/linux/amd64/yc" "$YC_SHA256" yc
  $SUDO install -m 0755 "$TMP/yc" "$BIN/yc"
}

for tool in "$@"; do
  echo "--- установка: $tool"
  start=$SECONDS
  case "$tool" in
    go) install_go ;;
    node) install_node ;;
    task) install_task ;;
    golangci-lint) install_golangci_lint ;;
    gitleaks) install_gitleaks ;;
    terraform) install_terraform ;;
    yc) install_yc ;;
    *)
      echo "неизвестный инструмент: $tool" >&2
      exit 2
      ;;
  esac
  echo "--- $tool: $((SECONDS - start)) с"
done

echo "--- версии"
# Не через head: head закрывает канал после первой строки, команда получает SIGPIPE,
# и с pipefail скрипт падает (так упал CI 26.11.2026 на terraform version).
# sed читает вывод целиком.
for tool in "$@"; do
  case "$tool" in
    go) go version ;;
    node) echo "node $(node --version), pnpm $(pnpm --version)" ;;
    task) task --version ;;
    golangci-lint) golangci-lint version | sed -n 1p ;;
    gitleaks) echo "gitleaks $(gitleaks version)" ;;
    terraform) CHECKPOINT_DISABLE=1 terraform version | sed -n 1p ;;
    yc) yc version ;;
  esac
done
command -v docker >/dev/null && docker --version || echo "docker: нет"
