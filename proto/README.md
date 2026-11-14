# proto

Контракт клиента и сервера в Protobuf. Любое изменение формата сообщений начинается здесь, код генерируется.

- `coffeechat/v1/system.proto` — служебный сервис `SystemService.Ping`.
- `coffeechat/v1/ws.proto` — кадры WebSocket: `ClientFrame`, `ServerFrame`, `Echo`, `Error`.

После правки схем: `task proto`. Команда проверяет схемы (`buf lint`) и заново создаёт код в `server/gen/` (Go) и `packages/proto/gen/` (TypeScript). Сгенерированный код коммитится.

Настройки: `buf.yaml` и `buf.gen.yaml` в корне репозитория. Плагины генерации локальные, версии зафиксированы в `server/go.mod` (директивы `tool`) и в корневом `package.json`.
