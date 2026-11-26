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

### Рубильник: остановить стенд

Стенд открыт всему интернету, а каждый запрос к шлюзу стоит денег. Если кто-то начал слать запросы без остановки:

```
task stop ENV=prod     # удалить шлюз: запросы перестают доходить и тарифицироваться
task start ENV=prod    # включить обратно
```

- `task stop` удаляет только шлюз. Контейнер, база, образ и фронтенд остаются; страница откроется, но все строки будут красными.
- После `task start` у шлюза **новый адрес**. Команда сама пересобирает и выкладывает фронтенд с этим адресом и запускает дымовой тест.
- Остановленный стенд остаётся остановленным и после `task deploy`: деплой обновит контейнер и базу, но шлюз не включит.
- Подтверждения эти команды не спрашивают: рубильник должен срабатывать сразу.
- `task stop` и `task start` испытаны на `staging` 16–17.11.2026. Однажды `task start` несколько раз подряд падал с `PERMISSION_DENIED` при создании шлюза — тогда не создавался даже пустой шлюз-заглушка, то есть причина была не в нашей конфигурации. Через несколько часов, после повторного входа `yc`, команда прошла. Причина не установлена. Если повторится: перезайти в `yc` (`yc init` или повторный запуск с входом через браузер), подождать и повторить.

Как узнать, что пора: бюджет платёжного аккаунта присылает письмо на порогах 50, 80 и 100 %. Быстрее — оповещение в мониторинге Yandex Cloud на число запросов к шлюзу (настраивается в консоли вручную, в Terraform его нет).

- Адреса стенда после деплоя лежат в `.secrets/<стенд>.outputs.env`; их же показывает `task tf ENV=staging -- output`.
- Бакет `coffeechat-staging` создан вручную, поэтому Terraform его не создаёт, а берёт под управление (блок `import`). Бакет `coffeechat` для `prod` создаёт сам Terraform.
- Terraform проверяет, что каталог из `.secrets/<стенд>.env` называется так же, как стенд, и иначе останавливается: перепутанный `YC_FOLDER_ID` ничего не создаст.
- Одновременно два деплоя одного стенда запускать нельзя: блокировки state нет, а очереди запусков у CI SourceCraft тоже нет (см. «CI»).

## CI

Конфигурация — `.sourcecraft/ci.yaml`, скрипты для воркера — `deploy/ci/`.

| Событие | Workflow | Что делает |
| --- | --- | --- |
| pull request в `main` | `checks` | gitleaks, `task lint`, `buf breaking`, тесты с покрытием (Go с `-race`, TS), контрактные тесты на `local-ydb`, сборка фронтенда (размер бандла) и образа, `task audit` (не блокирует) |
| push в `main` | `staging` | проверка ветки, те же проверки без сборки и audit, `task deploy ENV=staging`, хэш сборки фронтенда, push `main` на GitHub |
| тег `v*` | `prod` | проверка «коммит тега есть в `main`», `task deploy ENV=prod`, хэш сборки, push тега на GitHub |

- Вход в облако — через **сервисные подключения** SourceCraft: CI получает IAM-токен `sa-deploy-<стенд>` на время запуска. Ключей сервисных аккаунтов в секретах нет.
- `Taskfile.yml` берёт токен из `YC_IAM_TOKEN`, если переменная задана (CI), иначе из `yc iam create-token` (ПК).
- Инструменты ставит `deploy/ci/setup.sh`: версии и контрольные суммы зафиксированы в нём. Кэша между запусками нет.
- Встроенного зеркала из SourceCraft в GitHub нет (есть только синхронизация в обратную сторону), поэтому зеркало — `git push` с токеном GitHub (`deploy/ci/mirror.sh`).
- Триггеры срабатывают, только когда `.sourcecraft/ci.yaml` уже лежит в `main`. Поэтому первый pull request с этим файлом проверки не запустит, а его слияние сразу выкатит `staging`.

### Что настроить один раз

**1. Права `sa-deploy-*`** — роль `admin` на свой каталог (решение от 16.11.2026):

```
yc resource-manager folder add-access-binding staging --role admin --subject serviceAccount:<ID sa-deploy-staging>
yc resource-manager folder add-access-binding prod --role admin --subject serviceAccount:<ID sa-deploy-prod>
```

ID аккаунта: `yc iam service-account get sa-deploy-staging --folder-name staging`.

**2. Сервисные подключения** — в репозитории SourceCraft: настройки → сервисные подключения → создать два:

| Имя (как в `ci.yaml`) | Каталог | Сервисный аккаунт | Область |
| --- | --- | --- | --- |
| `coffeechat-staging` | `staging` | `sa-deploy-staging` | репозиторий `coffeechat`, ветка `main` |
| `coffeechat-prod` | `prod` | `sa-deploy-prod` | репозиторий `coffeechat`, **все ветки** (без ограничения) |

- Область подключения и `scope` в `ci.yaml` должны совпадать, иначе вход падает с 401 «No matching federated credentials». Подключение с веткой `main` — `scope: ref`, без ограничения ветки — `scope: repo`. Что ожидает федерация, видно в консоли: IAM → «Федерации сервисных аккаунтов» → федерация → «Привязанные сервисные аккаунты», колонка «Значение Sub».
- Ограничить подключение тегами нельзя, а запуск по тегу идёт от `refs/tags/v…`. Поэтому подключение `prod` открыто для всего репозитория, и на `prod` пускает только кубик `check-ref`: тег `v*`, коммит которого есть в `main`. Остаточный риск: запуск из любой ветки может запросить токен `prod`, но пушить в репозиторий может только автор.

SourceCraft сам создаёт в облаке федерацию удостоверений для каждого подключения.

**3. Секреты репозитория** — настройки → секреты:

| Секрет | Значение |
| --- | --- |
| `STAGING_TFSTATE_KEY_ID` | `AWS_ACCESS_KEY_ID` из `.secrets/staging.env` |
| `STAGING_TFSTATE_SECRET` | `AWS_SECRET_ACCESS_KEY` из `.secrets/staging.env` |
| `PROD_TFSTATE_KEY_ID` | `AWS_ACCESS_KEY_ID` из `.secrets/prod.env` |
| `PROD_TFSTATE_SECRET` | `AWS_SECRET_ACCESS_KEY` из `.secrets/prod.env` |
| `GH_MIRROR_TOKEN` | токен GitHub для зеркала (ниже) |

**4. Токен GitHub.** github.com → Settings → Developer settings → Fine-grained tokens → Generate:

- Repository access: Only select repositories → `KiraFlamov/CoffeeChat`.
- Permissions → Repository → Contents: **Read and write**. Остальное не нужно.
- Срок жизни — не больше года; перед истечением выпустить новый и заменить секрет.
- Если в репозитории на GitHub уже есть коммиты (README при создании), первый push отклонится. Тогда один раз с ПК: `git push --force https://github.com/KiraFlamov/CoffeeChat.git main`.

### Проверка (критерии Т10)

1. Ветка с ошибкой линтера → pull request → workflow `checks` красный.
2. Слияние в `main` → `staging` выкатился без ручных шагов; в логе кубика `deploy` — хэш сборки фронтенда; коммит появился на GitHub.
3. `git tag v0.0.1 && git push origin v0.0.1` → `prod` выкатился с тем же коммитом.
4. Тег на коммит не из `main` → кубик `check-ref` красный, деплоя нет.

### Ограничения

- Очереди нет: следующий PR сливать и следующий тег ставить после конца предыдущего деплоя.
- Минуты: 1000 в месяц на организацию. Длительность запусков записать после первых прогонов в `docs/limits.md`.
