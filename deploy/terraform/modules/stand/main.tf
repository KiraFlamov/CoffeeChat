# Один стенд: всё, что нужно серверу и фронтенду в одном каталоге Yandex Cloud.
# Каталог берётся из настроек провайдера (переменная окружения YC_FOLDER_ID).

data "yandex_client_config" "this" {}

# Защита от ошибки в .secrets/<стенд>.env: если YC_FOLDER_ID указывает на чужой каталог,
# план останавливается здесь и ничего не создаёт.
data "yandex_resourcemanager_folder" "this" {
  folder_id = data.yandex_client_config.this.folder_id

  lifecycle {
    postcondition {
      condition     = self.name == var.folder_name
      error_message = "Стенд должен разворачиваться в каталоге «${var.folder_name}», а YC_FOLDER_ID указывает на каталог «${self.name}». Проверь файл .secrets/<стенд>.env."
    }
  }
}

locals {
  folder_id  = data.yandex_resourcemanager_folder.this.id
  name       = "coffeechat"
  web_origin = "https://${var.web_bucket}.website.yandexcloud.net"
  # Контейнер и шлюз создаются, только когда в реестре уже есть образ.
  deploy_app = var.image_tag != ""
  image_repo = "cr.yandex/${yandex_container_registry.this.id}/${local.name}"
}

# --- Сервисные аккаунты ---------------------------------------------------

# Имя сервисного аккаунта должно быть уникальным во всём облаке, а не в каталоге,
# поэтому в имени есть стенд.

# От его имени работает сервер: ходит в базу, в Send API и скачивает свой образ.
resource "yandex_iam_service_account" "app" {
  name        = "sa-app-${var.folder_name}"
  description = "Сервер coffeechat в Serverless Containers"
}

# От его имени шлюз вызывает контейнер. Других прав у него нет.
resource "yandex_iam_service_account" "gateway" {
  name        = "sa-gateway-${var.folder_name}"
  description = "API Gateway coffeechat: вызов контейнера"
}

# --- База -----------------------------------------------------------------

resource "yandex_ydb_database_serverless" "this" {
  name                = local.name
  deletion_protection = var.deletion_protection

  serverless_database {
    # Предел хранилища в гигабайтах. Бесплатно — 1 ГБ: больше база вырасти не сможет.
    storage_size_limit = 1
  }
}

# Права сервера только на эту базу, а не на все базы каталога.
resource "yandex_ydb_database_iam_member" "app" {
  database_id = yandex_ydb_database_serverless.this.id
  role        = "ydb.editor"
  member      = "serviceAccount:${yandex_iam_service_account.app.id}"
}

# --- Реестр образов -------------------------------------------------------

resource "yandex_container_registry" "this" {
  name = local.name
}

resource "yandex_container_repository" "server" {
  name = "${yandex_container_registry.this.id}/${local.name}"
}

# Хранение образов платное: держим только последние.
resource "yandex_container_repository_lifecycle_policy" "server" {
  name          = "keep-last-images"
  status        = "active"
  repository_id = yandex_container_repository.server.id

  rule {
    description  = "Оставлять 5 последних образов с тегом"
    tag_regexp   = ".*"
    retained_top = 5
  }

  rule {
    description   = "Удалять образы без тега через двое суток"
    untagged      = true
    expire_period = "48h"
  }
}

resource "yandex_container_registry_iam_binding" "puller" {
  registry_id = yandex_container_registry.this.id
  role        = "container-registry.images.puller"
  members     = ["serviceAccount:${yandex_iam_service_account.app.id}"]
}

# --- Сервер ---------------------------------------------------------------

# Send API шлюза: роль выдаётся на каталог (проверено на разведке, ADR-002).
resource "yandex_resourcemanager_folder_iam_member" "app_websocket_writer" {
  folder_id = local.folder_id
  role      = "api-gateway.websocketWriter"
  member    = "serviceAccount:${yandex_iam_service_account.app.id}"
}

resource "yandex_serverless_container" "server" {
  count = local.deploy_app ? 1 : 0

  name               = local.name
  memory             = 128
  cores              = 1
  core_fraction      = 100
  execution_timeout  = "10s"
  concurrency        = var.container_concurrency
  service_account_id = yandex_iam_service_account.app.id

  runtime {
    type = "http"
  }

  image {
    url = "${local.image_repo}:${var.image_tag}"
    environment = {
      YDB_DSN = yandex_ydb_database_serverless.this.ydb_full_endpoint
    }
  }

  # Права должны быть выданы до первого запуска контейнера.
  depends_on = [
    yandex_container_registry_iam_binding.puller,
    yandex_ydb_database_iam_member.app,
    yandex_resourcemanager_folder_iam_member.app_websocket_writer,
  ]
}

resource "yandex_serverless_container_iam_member" "gateway_invoker" {
  count = local.deploy_app ? 1 : 0

  container_id = yandex_serverless_container.server[0].id
  role         = "serverless-containers.containerInvoker"
  member       = "serviceAccount:${yandex_iam_service_account.gateway.id}"
}

# --- Шлюз -----------------------------------------------------------------

resource "yandex_api_gateway" "this" {
  count = local.deploy_app ? 1 : 0

  name = local.name
  spec = templatefile("${path.module}/../../../gateway.yaml", {
    web_origin    = local.web_origin
    container_id  = yandex_serverless_container.server[0].id
    gateway_sa_id = yandex_iam_service_account.gateway.id
  })

  depends_on = [yandex_serverless_container_iam_member.gateway_invoker]
}

# --- Фронтенд -------------------------------------------------------------

# Публичный бакет со статическими файлами сборки. Список объектов закрыт.
resource "yandex_storage_bucket" "web" {
  bucket    = var.web_bucket
  folder_id = local.folder_id
  # Предел размера 100 МБ: сборка занимает меньше 1 МБ, а предел защищает от случайных расходов.
  max_size = 104857600

  anonymous_access_flags {
    read        = true
    list        = false
    config_read = false
  }

  website {
    index_document = "index.html"
    error_document = "index.html"
  }
}
