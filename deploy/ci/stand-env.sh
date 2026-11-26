#!/usr/bin/env bash
# Готовит файл .secrets/<стенд>.env на воркере CI — тот же, что у автора на ПК
# (deploy/README.md). Значения берутся из переменных окружения кубика:
#   YC_CLOUD_ID, YC_FOLDER_ID — из сервисного подключения SourceCraft;
#   AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY — статический ключ к бакету state, секреты CI.
# Запуск: deploy/ci/stand-env.sh staging
set -euo pipefail

stand=${1:?укажи стенд: staging или prod}
case "$stand" in
  staging | prod) ;;
  *)
    echo "неизвестный стенд: $stand" >&2
    exit 2
    ;;
esac

missing=""
for name in YC_IAM_TOKEN YC_CLOUD_ID YC_FOLDER_ID AWS_ACCESS_KEY_ID AWS_SECRET_ACCESS_KEY; do
  if [ -z "${!name:-}" ]; then missing="$missing $name"; fi
done
if [ -n "$missing" ]; then
  echo "не заданы переменные:$missing (сервисное подключение и секреты CI, deploy/README.md)" >&2
  exit 1
fi

mkdir -p .secrets
umask 077
cat >".secrets/$stand.env" <<EOF
YC_CLOUD_ID=$YC_CLOUD_ID
YC_FOLDER_ID=$YC_FOLDER_ID
AWS_ACCESS_KEY_ID=$AWS_ACCESS_KEY_ID
AWS_SECRET_ACCESS_KEY=$AWS_SECRET_ACCESS_KEY
EOF
# Значения не печатаем: только то, что файл готов.
echo "файл .secrets/$stand.env готов, каталог $YC_FOLDER_ID"
