# Список изменений

Формат — [Keep a Changelog](https://keepachangelog.com/ru/1.1.0/), версии — [SemVer](https://semver.org/lang/ru/).

## [Не выпущено]

### Добавлено

- Каркас монорепозитория: модуль Go `server/`, рабочее пространство pnpm (`web`, `packages/crypto`, `packages/proto`), команды `task lint`, `task test`, `task fmt`.
- Линтеры и форматирование: golangci-lint, ESLint, Prettier.
- Контракт `proto/coffeechat/v1`: сервис `SystemService.Ping` и кадры WebSocket `Echo`; команда `task proto` генерирует код Go (`server/gen`) и TypeScript (`packages/proto/gen`); `buf lint` входит в `task lint`.
- Сервер: порты `Store` и `Transport`, ядро `core.System` (`Ping`, `OnConnect`, `OnDisconnect`, `OnMessage`) с unit-тестами.
- База: локальный YDB в Docker (`task db:up`, `task db:down`), миграции (`task migrate`), адаптер `store/ydb`, контрактные тесты `Store` (`task test:store`).
- Локальный сервер `cmd/server`: HTTP API (ConnectRPC) и WebSocket на `/ws`, транспорт в памяти `transport/hub`, CORS для `http://localhost:5173`; команда `task dev` поднимает базу, применяет миграции и запускает сервер.
- Веб-клиент `web/`: Vite, React, Tailwind; страница «Состояние системы» с пятью проверками (HTTP API, база, WebSocket двумя способами, воркеры) и кнопкой «Повторить». Адреса API задаются переменными сборки `VITE_API_URL` и `VITE_WS_URL`.
- `packages/crypto`: Web Worker с заглушкой, вызов через Comlink. Пустой Service Worker для проверки регистрации.
- `task dev` запускает ещё и Vite; добавлены `task dev:web` и `task build:web`.
- Сервер для облака `cmd/container`: HTTP API и события WebSocket от API Gateway (`CONNECT`, `MESSAGE`, `DISCONNECT`); адаптер `transport/yc` отправляет кадры через Send API с IAM-токеном из сервиса метаданных.
- `task image` собирает образ контейнера без Docker (сборщик `deploy/image`): один статический бинарник со встроенными корневыми сертификатами, 8,8 МБ.
- Terraform (`deploy/terraform`): модуль стенда и окружения `staging` и `prod` с отдельным state; спецификация шлюза `deploy/gateway.yaml`.
- Команды облака: `task deploy ENV=…` (стенд целиком), `task deploy-web`, `task migrate:cloud`, `task tf`, `task smoke` — дымовой тест стенда (`cmd/smoke`).
- Сборщик образа умеет отправлять образ в реестр (`-push`); `admin migrate` входит в облачную базу по IAM-токену из `YDB_ACCESS_TOKEN`.
- Миграции применяет свой минимальный применятель вместо goose: файлы по порядку номеров, применённые — в таблице `schema_migrations`.
- Выгрузка фронтенда — своей командой `cmd/webupload`: по одному файлу, с явным именем объекта и типом содержимого. Дымовой тест проверяет, что скрипты и стили страницы отдаются с правильным типом.
- Рубильник стенда: `task stop ENV=…` удаляет шлюз, `task start ENV=…` включает обратно; остановленный стенд не включается при деплое.
- Terraform проверяет, что стенд разворачивается в каталоге с тем же именем; сервисные аккаунты названы по стендам.
- CI в SourceCraft (`.sourcecraft/ci.yaml`): проверки pull request (gitleaks, линтеры, `buf breaking`, тесты с покрытием, контрактные тесты на `local-ydb`, сборка, размер бандла, проверка уязвимостей); push в `main` — деплой на `staging` и зеркало на GitHub; тег `v*` из `main` — деплой на `prod`. Вход в облако — через сервисные подключения, без ключей сервисных аккаунтов.
- Команды `task test:cover` (покрытие Go и TypeScript) и `task audit` (govulncheck и pnpm audit); зависимость `@vitest/coverage-v8`.
- Команды облака берут IAM-токен из `YC_IAM_TOKEN`, если он задан (CI), иначе из `yc iam create-token`.
