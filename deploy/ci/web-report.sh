#!/usr/bin/env bash
# Отчёт о сборке фронтенда для лога CI: размер бандла и хэш сборки.
# Хэш сборки — sha256 от списка «sha256 файла и путь» всех файлов web/dist.
# Сборки staging и prod различаются адресами API, поэтому хэш у каждого стенда свой.
# Запуск: deploy/ci/web-report.sh <подпись>, например staging.
set -euo pipefail

label=${1:-сборка}
dist=web/dist
if [ ! -d "$dist" ]; then
  echo "[$label] сборки фронтенда нет ($dist) — стенд остановлен или сборка не запускалась"
  exit 0
fi

cd "$dist"
files=$(find . -type f | LC_ALL=C sort)
build_hash=$(echo "$files" | xargs sha256sum | sha256sum | cut -d' ' -f1)
total=$(echo "$files" | xargs cat | wc -c)
js=$(find . -type f \( -name '*.js' -o -name '*.mjs' \) -print0 | xargs -0 -r cat | wc -c)
js_gz=$(find . -type f \( -name '*.js' -o -name '*.mjs' \) -print0 | xargs -0 -r cat | gzip -9 | wc -c)
css=$(find . -type f -name '*.css' -print0 | xargs -0 -r cat | wc -c)

echo "[$label] хэш сборки фронтенда: $build_hash"
echo "[$label] размер: всего $total Б, JS $js Б (gzip $js_gz Б), CSS $css Б"
echo "[$label] файлы:"
echo "$files" | xargs ls -l | awk '{printf "  %8d  %s\n", $5, $9}'
