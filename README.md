# Authentication

Проект на основе проекта real-time-forum. Можно зайти на форум через Github и Gmail.

## Структура проекта 

```
main.go                     — точка входа
internal/
  handlers/                 — HTTP-обработчики
  chat/                     — хаб WebSocket-соединений (real-time события)
  models/                   — структуры данных
  repository/               — слой работы с SQLite
  server/                   — сборка mux и маршрутов
  templating/               — кэш шаблонов
ui/
  templates/                — HTML-шаблоны (layout + страницы)
  static/css/               — стили
  static/js/realtime.js     — real-time клиент (WebSocket: сообщения, тосты, пагинация, счётчики реакций)
```

## Валидация на сервере

- Email проверяется по RFC-адресу и OATH
- Username непустой (до 40 символов) и не занят
- Пароль — минимум 6 символов
- Длины заголовка/текста поста и комментария ограничены


### Запуск через Docker compose

```bash
docker compose build # можно пропустить, если собираете впервые
docker compose up    # запуск
```

## Учётные данные для проверки

Записаны на странице входа (login). Можно также зарегистрировать нового пользователя.

