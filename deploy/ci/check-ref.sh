#!/usr/bin/env bash
# Проверяет, что запуск CI пришёл из нужной ветки или тега, до любых действий в облаке.
#   deploy/ci/check-ref.sh main  — запуск из ветки main (деплой на staging);
#   deploy/ci/check-ref.sh tag   — запуск из тега v*, коммит которого лежит в main (деплой на prod).
# Ссылку на коммит даёт CI SourceCraft: SOURCECRAFT_COMMIT_REF (refs/heads/main, refs/tags/v0.1.0).
set -euo pipefail

mode=${1:?укажи режим: main или tag}
ref=${SOURCECRAFT_COMMIT_REF:?нет SOURCECRAFT_COMMIT_REF: скрипт рассчитан на CI SourceCraft}

case "$mode" in
  main)
    if [ "$ref" != "refs/heads/main" ]; then
      echo "деплой на staging только из main, а запуск из $ref" >&2
      exit 1
    fi
    ;;
  tag)
    case "$ref" in
      refs/tags/v*) ;;
      *)
        echo "деплой на prod только по тегу v*, а запуск из $ref" >&2
        exit 1
        ;;
    esac
    # Клон в CI может быть неполным: дотягиваем историю и свежий main.
    if [ "$(git rev-parse --is-shallow-repository)" = "true" ]; then
      git fetch --quiet --unshallow --no-tags origin
    fi
    git fetch --quiet --no-tags origin +refs/heads/main:refs/remotes/origin/main
    commit=$(git rev-parse HEAD)
    if ! git merge-base --is-ancestor "$commit" origin/main; then
      echo "тег ${ref#refs/tags/} указывает на коммит $commit, которого нет в main — деплой на prod отклонён" >&2
      exit 1
    fi
    echo "тег ${ref#refs/tags/}: коммит $commit есть в main"
    ;;
  *)
    echo "неизвестный режим: $mode" >&2
    exit 2
    ;;
esac
