# Build context is order-service/ itself (see compose.yaml), as for
# user-service: the image needs nothing from outside this folder.

# Build stage
FROM golang:1.26-alpine AS builder
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/order-service ./cmd/api

# Runtime stage
FROM alpine:3.22

RUN adduser -D -H appuser

WORKDIR /app

COPY --from=builder /out/order-service ./order-service
# cmd/api applies these at startup, from ./migrations under the working
# directory or /app/migrations.
COPY migrations ./migrations

USER appuser

# Listens on PORT, 8083 in compose.yaml (root AGENTS.md §3).
EXPOSE 8083

ENTRYPOINT ["./order-service"]
