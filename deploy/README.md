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

## Стенды в облаке

Два стенда: `staging` и `prod`, каждый в своём каталоге Yandex Cloud. Описание — Terraform:

| Путь                              | Содержимое                                                            |
| --------------------------------- | --------------------------------------------------------------------- |
| `terraform/modules/stand/`        | один стенд: сервисные аккаунты, база, реестр, контейнер, шлюз, бакет  |
| `terraform/envs/staging/`, `prod/` | настройки стенда: где лежит state, имя бакета фронтенда               |
| `gateway.yaml`                    | спецификация API Gateway (шаблон, значения подставляет Terraform)     |

### Что нужно один раз

**1. Terraform.** Сайт HashiCorp из России закрыт, поэтому берём с зеркала Яндекса.

- Скачать `https://hashicorp-releases.yandexcloud.net/terraform/1.16.0/terraform_1.16.0_windows_amd64.zip`, распаковать `terraform.exe` в папку из `PATH`.
- Создать файл `%APPDATA%\terraform.rc` (в Linux — `~/.terraformrc`), чтобы провайдер тоже скачивался с зеркала:

```
provider_installation {
  network_mirror {
    url     = "https://terraform-mirror.yandexcloud.net/"
    include = ["registry.terraform.io/*/*"]
  }
  direct {
    exclude = ["registry.terraform.io/*/*"]
  }
}
```

**2. Бакет для state и ключ к нему** — для каждого стенда свои. Пример для `staging` (для `prod` заменить имя везде):

```
yc iam service-account create --name sa-deploy-staging --folder-name staging
yc resource-manager folder add-access-binding staging --role storage.editor --subject serviceAccount:<ID аккаунта из прошлой команды>
yc iam access-key create --service-account-name sa-deploy-staging --folder-name staging
yc storage bucket create --name coffeechat-tfstate-staging --folder-name staging
```

- Третья команда печатает `key_id` и `secret` — секрет показывается один раз.
- Если имя бакета занято, выбрать другое и поправить `bucket` в `terraform/envs/staging/main.tf`.

**3. Файл `.secrets/staging.env`** (в репозиторий не попадает):

```
YC_CLOUD_ID=<ID облака: yc config list>
YC_FOLDER_ID=<ID каталога: yc resource-manager folder list>
AWS_ACCESS_KEY_ID=<key_id>
AWS_SECRET_ACCESS_KEY=<secret>
```

Ключ нужен только для доступа к state. Всё остальное Terraform делает от твоего имени: токен на каждый запуск выдаёт `yc iam create-token`.

### Деплой

```
task deploy ENV=staging
```

Команда по шагам:

1. `terraform init`.
2. Первый проход Terraform с тегом уже развёрнутого образа: при первом деплое создаёт всё, кроме контейнера и шлюза (реестр, базу, права), позже ничего не меняет.
3. Собирает образ (`task image`) и отправляет его в реестр.
4. Применяет миграции к базе стенда.
5. Второй проход Terraform: контейнер с новым образом и шлюз.
6. Собирает фронтенд с адресами стенда и выгружает в бакет (`task deploy-web`).
7. Запускает дымовой тест (`task smoke`).

Terraform дважды покажет план и спросит подтверждение — отвечать `yes`. Без вопросов: `task deploy ENV=staging APPROVE=-auto-approve`.

Отдельные шаги:

```
task tf ENV=staging -- plan       # любая команда terraform для стенда
task migrate:cloud ENV=staging    # только миграции
task deploy-web ENV=staging       # только фронтенд
task smoke ENV=staging            # только дымовой тест
```

- Адреса стенда после деплоя лежат в `.secrets/<стенд>.outputs.env`; их же показывает `task tf ENV=staging -- output`.
- Бакеты фронтенда `coffeechat` и `coffeechat-staging` созданы вручную, поэтому Terraform их не создаёт, а берёт под управление (блок `import`).
- Одновременно два деплоя одного стенда запускать нельзя: блокировки state пока нет (решить на Т10).
