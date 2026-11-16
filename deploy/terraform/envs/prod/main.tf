# Стенд prod. Запуск — через task (см. deploy/README.md), а не terraform напрямую:
# task подставляет токен и ключи из .secrets/prod.env.

terraform {
  required_version = ">= 1.10"

  required_providers {
    yandex = {
      source  = "yandex-cloud/yandex"
      version = "0.248.0"
    }
  }

  # State хранится в приватном бакете Object Storage, отдельном для каждого стенда.
  # Ключи доступа — переменные окружения AWS_ACCESS_KEY_ID и AWS_SECRET_ACCESS_KEY.
  backend "s3" {
    endpoints = {
      s3 = "https://storage.yandexcloud.net"
    }
    bucket = "coffeechat-tfstate-prod"
    key    = "terraform.tfstate"
    region = "ru-central1"

    skip_region_validation      = true
    skip_credentials_validation = true
    skip_requesting_account_id  = true
    skip_s3_checksum            = true
  }
}

# Облако, каталог и токен — из переменных окружения YC_CLOUD_ID, YC_FOLDER_ID, YC_TOKEN.
provider "yandex" {}

variable "image_tag" {
  description = "Тег образа сервера. Пусто — контейнер и шлюз не создаются."
  type        = string
  default     = ""
}

module "stand" {
  source = "../../modules/stand"

  folder_name         = "prod"
  web_bucket          = "coffeechat"
  image_tag           = var.image_tag
  deletion_protection = true
}

# Бакет фронтенда создаёт Terraform. Вручную созданный бакет с этим именем лежал
# в каталоге staging; 16.11.2026 он пересоздан в каталоге prod.

output "api_url" {
  value = module.stand.api_url
}

output "ws_url" {
  value = module.stand.ws_url
}

output "image_tag" {
  value = module.stand.image_tag
}

output "web_url" {
  value = module.stand.web_url
}

output "deploy_env" {
  value = module.stand.deploy_env
}

output "web_env" {
  value = module.stand.web_env
}
