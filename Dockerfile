# syntax=docker/dockerfile:1
FROM golang:1.26-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /app/bot ./cmd/bot/

FROM alpine:3.21

RUN apk --no-cache add ca-certificates tzdata

WORKDIR /app
COPY --from=builder /app/bot .

# Директория для SQLite БД
RUN mkdir -p /app/data

EXPOSE 8080

CMD ["./bot"]
