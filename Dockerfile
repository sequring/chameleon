FROM golang:1.24-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download && go mod verify

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /chameleon_server ./main.go


FROM alpine:latest
WORKDIR /app
COPY --from=builder /chameleon_server /app/chameleon_server

COPY config.yml ./config.yml
COPY proxies.json ./proxies.json
COPY users.json ./users.json

RUN mkdir -p /app/logs && \
  chown -R nobody:nogroup /app/logs || true # Попытка сменить владельца, если nobody существует

EXPOSE 1080 8081

ENTRYPOINT ["/app/chameleon_server"]
CMD ["-config", "/app/config.yml"]