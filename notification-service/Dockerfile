# AI Assistance Disclosure:
# Tool: Claude Code (model: Opus 5.5), date: 2026-11-05
# Scope: Image for notification-service, as for order-service.
# Author review: nigeltzy - checked that the changes made sense and were consistent with the rest of the file.

# Build context is notification-service/ itself (see compose.yaml).

# Build stage
FROM golang:1.26-alpine AS builder
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/notification-service ./cmd/api

# Runtime stage
FROM alpine:3.22

RUN adduser -D -H appuser

WORKDIR /app

COPY --from=builder /out/notification-service ./notification-service
# cmd/api applies these at startup.
COPY migrations ./migrations

USER appuser

# Listens on PORT, 8086 in compose.yaml (root AGENTS.md §3).
EXPOSE 8086

ENTRYPOINT ["./notification-service"]
