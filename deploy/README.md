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

Спецификация шлюза и Terraform появятся в задаче Т9 этапа 0.
