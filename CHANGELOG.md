# Список изменений

Формат — [Keep a Changelog](https://keepachangelog.com/ru/1.1.0/), версии — [SemVer](https://semver.org/lang/ru/).

## [Не выпущено]

### Добавлено

- Каркас монорепозитория: модуль Go `server/`, рабочее пространство pnpm (`web`, `packages/crypto`, `packages/proto`), команды `task lint`, `task test`, `task fmt`.
- Линтеры и форматирование: golangci-lint, ESLint, Prettier.
- Контракт `proto/coffeechat/v1`: сервис `SystemService.Ping` и кадры WebSocket `Echo`; команда `task proto` генерирует код Go (`server/gen`) и TypeScript (`packages/proto/gen`); `buf lint` входит в `task lint`.
- Сервер: порты `Store` и `Transport`, ядро `core.System` (`Ping`, `OnConnect`, `OnDisconnect`, `OnMessage`) с unit-тестами.
- База: локальный YDB в Docker (`task db:up`, `task db:down`), миграции через goose (`task migrate`), адаптер `store/ydb`, контрактные тесты `Store` (`task test:store`).
- Локальный сервер `cmd/server`: HTTP API (ConnectRPC) и WebSocket на `/ws`, транспорт в памяти `transport/hub`, CORS для `http://localhost:5173`; команда `task dev` поднимает базу, применяет миграции и запускает сервер.
- Веб-клиент `web/`: Vite, React, Tailwind; страница «Состояние системы» с пятью проверками (HTTP API, база, WebSocket двумя способами, воркеры) и кнопкой «Повторить». Адреса API задаются переменными сборки `VITE_API_URL` и `VITE_WS_URL`.
- `packages/crypto`: Web Worker с заглушкой, вызов через Comlink. Пустой Service Worker для проверки регистрации.
- `task dev` запускает ещё и Vite; добавлены `task dev:web` и `task build:web`.
- Сервер для облака `cmd/container`: HTTP API и события WebSocket от API Gateway (`CONNECT`, `MESSAGE`, `DISCONNECT`); адаптер `transport/yc` отправляет кадры через Send API с IAM-токеном из сервиса метаданных.
- `task image` собирает образ контейнера без Docker (сборщик `deploy/image`): один статический бинарник со встроенными корневыми сертификатами, 8,8 МБ.
