# deploy

Инфраструктура: docker-compose для локального запуска, спецификация API Gateway, Terraform.

## Локальная база

`docker-compose.yml` поднимает YDB в одном контейнере (`ydbplatform/local-ydb`).

```
task db:up        # запустить базу
task migrate      # создать таблицы
task test:store   # обе команды выше и контрактные тесты адаптера
task db:down      # остановить
```

- Строка подключения: `grpc://localhost:2136/local` (переменная `YDB_DSN`).
- Веб-интерфейс базы: http://localhost:8765
- Данные хранятся в памяти: после перезапуска контейнера нужно снова выполнить `task migrate`.

## Локальный сервер

```
task dev          # база, миграции и сервер одной командой; остановка — Ctrl+C
task dev:server   # только сервер, если база уже запущена
```

- HTTP API: `http://localhost:8080`, WebSocket: `ws://localhost:8080/ws`.
- Проверка: `curl -X POST -H "Content-Type: application/json" -d "{}" http://localhost:8080/coffeechat.v1.SystemService/Ping` — в ответе `"storeOk":true`.
- Переменные окружения сервера: `ADDR`, `YDB_DSN`, `CORS_ORIGIN` (по умолчанию `localhost:8080`, локальная база, `http://localhost:5173`).
- База после Ctrl+C продолжает работать; остановить её — `task db:down`.

## Образ контейнера

```
task image   # собрать образ сервера, Docker не нужен
```

- Результат: `server/bin/coffeechat-image.tar` (формат `docker save`), тег — короткий хэш текущего коммита.
- Внутри один файл `/app` — статический бинарник `cmd/container`; корневые сертификаты встроены в него.
- Сборщик — `deploy/image`, отдельный модуль Go. Почему так — `docs/adr/ADR-004-container-image.md`.

Спецификация шлюза и Terraform появятся в задаче Т9 этапа 0.
