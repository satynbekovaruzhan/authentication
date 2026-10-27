# Этап сборки (Builder)
FROM golang:alpine AS builder

WORKDIR /app

# Устанавливаем зависимости для CGO (необходимо для go-sqlite3)
RUN apk add --no-cache gcc musl-dev

# Копируем go.mod и go.sum и скачиваем зависимости
COPY go.mod go.sum ./
RUN go mod download

# Копируем исходный код
COPY . .

# Собираем бинарник с включенным CGO
RUN CGO_ENABLED=1 GOOS=linux go build -o forum main.go

# Финальный легковесный образ
FROM alpine:latest

WORKDIR /app

# Добавляем корневые сертификаты и часовые пояса на всякий случай
RUN apk --no-cache add ca-certificates tzdata

# Копируем скомпилированный бинарник из builder'а
COPY --from=builder /app/forum .

# Static files are copied into the runtime image; SQLite is created on first start.
COPY ui ./ui

# Указываем порт, на котором работает приложение
EXPOSE 8080

# Команда для запуска
CMD ["./forum"]
