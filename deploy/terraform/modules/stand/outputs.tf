locals {
  api_host = local.deploy_app ? yandex_api_gateway.this[0].domain : ""
  api_url  = local.deploy_app ? "https://${local.api_host}" : ""
  ws_url   = local.deploy_app ? "wss://${local.api_host}/ws" : ""
}

output "api_url" {
  description = "Адрес HTTP API. Пусто, пока контейнер не развёрнут."
  value       = local.api_url
}

output "ws_url" {
  description = "Адрес WebSocket. Пусто, пока контейнер не развёрнут."
  value       = local.ws_url
}

output "image_tag" {
  description = "Тег развёрнутого образа. Команда деплоя читает его, чтобы первый проход не трогал работающий контейнер."
  value       = var.image_tag
}

output "web_url" {
  description = "Адрес фронтенда."
  value       = local.web_origin
}

output "web_bucket" {
  description = "Бакет фронтенда."
  value       = yandex_storage_bucket.web.bucket
}

output "image_repository" {
  description = "Куда отправлять образ сервера (без тега)."
  value       = local.image_repo
}

output "ydb_dsn" {
  description = "Строка подключения к базе."
  value       = yandex_ydb_database_serverless.this.ydb_full_endpoint
}

# Те же значения в виде файлов: их читают команды деплоя (Taskfile.yml).

output "deploy_env" {
  description = "Переменные для команд деплоя, формат dotenv."
  value       = <<-EOT
    API_URL=${local.api_url}
    WS_URL=${local.ws_url}
    WEB_URL=${local.web_origin}
    WEB_BUCKET=${yandex_storage_bucket.web.bucket}
    IMAGE_REPOSITORY=${local.image_repo}
    YDB_DSN=${yandex_ydb_database_serverless.this.ydb_full_endpoint}
  EOT
}

output "web_env" {
  description = "Переменные сборки фронтенда, формат dotenv."
  value       = <<-EOT
    VITE_API_URL=${local.api_url}
    VITE_WS_URL=${local.ws_url}
  EOT
}
