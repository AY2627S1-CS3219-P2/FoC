# Build context is chat-service/ itself (see compose.yaml)

# Build stage
FROM golang:1.26-alpine AS builder
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/chat-service ./cmd/api

# Runtime stage
FROM alpine:3.22

RUN adduser -D -H appuser

WORKDIR /app

COPY --from=builder /out/chat-service ./chat-service
COPY migrations ./migrations

# Create media storage directory
RUN mkdir -p /var/lib/foc/chat-media && chown -R appuser:appuser /var/lib/foc/chat-media

USER appuser

# Listens on PORT, 8085 in compose.yaml
EXPOSE 8085

ENTRYPOINT ["./chat-service"]
