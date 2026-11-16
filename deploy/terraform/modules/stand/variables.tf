variable "folder_name" {
  description = "Имя каталога Yandex Cloud, в котором должен жить стенд. Защита от запуска не в том каталоге."
  type        = string
}

variable "web_bucket" {
  description = "Имя бакета фронтенда. От него зависит адрес сайта и разрешённый origin в CORS."
  type        = string
}

variable "image_tag" {
  description = "Тег образа сервера в реестре. Пустая строка — контейнер и шлюз не создаются (первый запуск, когда образа ещё нет)."
  type        = string
  default     = ""
}

variable "deletion_protection" {
  description = "Защита базы от удаления."
  type        = bool
  default     = false
}

variable "container_concurrency" {
  description = "Сколько запросов один экземпляр контейнера обрабатывает одновременно."
  type        = number
  default     = 8
}
