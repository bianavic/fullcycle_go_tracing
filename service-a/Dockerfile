# Build stage
FROM golang:1.27.1 AS builder

WORKDIR /app

COPY service-a/ ./

RUN go mod download

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o service-a ./cmd/server/main.go

# Runtime
FROM alpine:3.24
WORKDIR /app
COPY --from=builder /app/service-a .
EXPOSE 8080
HEALTHCHECK --interval=10s --timeout=3s --start-period=5s --retries=3 \
  CMD nc -z localhost 8080 || exit 1
CMD ["./service-a"]