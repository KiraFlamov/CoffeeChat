#!/usr/bin/env bash
# Зеркало на GitHub: отправляет текущий коммит в main или текущий тег.
# Встроенного зеркала из SourceCraft в GitHub нет (есть только обратная синхронизация),
# поэтому обычный git push.
#   deploy/ci/mirror.sh main  — HEAD в ветку main на GitHub;
#   deploy/ci/mirror.sh tag   — тег из SOURCECRAFT_COMMIT_REF.
# Нужны переменные GITHUB_REPO (владелец/имя) и GITHUB_TOKEN (секрет CI:
# fine-grained токен GitHub с правом Contents: Read and write на один репозиторий).
set -euo pipefail

mode=${1:?укажи режим: main или tag}
repo=${GITHUB_REPO:?нет GITHUB_REPO}
token=${GITHUB_TOKEN:?нет GITHUB_TOKEN: секрет CI, deploy/README.md}

case "$mode" in
  main) refspec="$(git rev-parse HEAD):refs/heads/main" ;;
  tag)
    ref=${SOURCECRAFT_COMMIT_REF:?нет SOURCECRAFT_COMMIT_REF}
    git fetch --quiet --no-tags origin "+$ref:$ref"
    refspec="$ref:$ref"
    ;;
  *)
    echo "неизвестный режим: $mode" >&2
    exit 2
    ;;
esac

# GitHub не принимает push из неполного клона, если у него нет общей истории.
if [ "$(git rev-parse --is-shallow-repository)" = "true" ]; then
  git fetch --quiet --unshallow --no-tags origin
fi

# Токен передаётся заголовком, а не в адресе: адрес попадает в сообщения об ошибках git.
auth=$(printf 'x-access-token:%s' "$token" | base64 -w0)
GIT_TERMINAL_PROMPT=0 git -c credential.helper= -c "http.https://github.com/.extraheader=Authorization: Basic $auth" \
  push "https://github.com/$repo.git" "$refspec"
echo "зеркало: $refspec → github.com/$repo"
