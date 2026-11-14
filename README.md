# coffeechat

Мессенджер со сквозным шифрованием для закрытого круга: сайт и PWA. Сервер хранит и пересылает только шифротекст.

> Проект в разработке. Сейчас идёт этап 0 — каркас: шифрования и переписки ещё нет.
> Проект и библиотека ts-mls не проходили независимый аудит безопасности.

[English version](README.en.md)

## Что внутри

| Папка              | Содержимое                                                    |
| ------------------ | ------------------------------------------------------------- |
| `proto/`           | Контракт клиента и сервера (Protobuf)                         |
| `web/`             | Веб-клиент: React, TypeScript, Vite                           |
| `packages/crypto/` | Вся криптография клиента                                      |
| `packages/proto/`  | Сгенерированные TypeScript-типы контракта                     |
| `server/`          | Сервер на Go                                                  |
| `deploy/`          | Инфраструктура: Terraform, docker-compose, спецификация шлюза |

## Что нужно для разработки

- Go 1.27 или новее
- Node.js 22 или новее и pnpm
- [Task](https://taskfile.dev) — запуск команд проекта (на Windows: `winget install Task.Task`)
- [golangci-lint](https://golangci-lint.run) 2.x

## Команды

```
pnpm install     # зависимости TypeScript
task lint        # линтеры Go и TypeScript, проверка форматирования
task test        # все тесты
task fmt         # форматирование
```

## Лицензия

[MIT](LICENSE)
