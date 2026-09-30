# AI Assistance Disclosure:
# Tool: Codex (GPT-5), date: 2026-09-18
# Scope: Added a multi-stage Docker build for the user-service API.
# Author review: COMPLETED BY ZI YANG

# Build stage
FROM golang:1.26-alpine AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build \
    -o /out/user-service \
    ./cmd/api

# Runtime stage
FROM alpine:3.22

RUN adduser -D -H appuser

WORKDIR /app

COPY --from=builder /out/user-service ./user-service
COPY migrations ./migrations

USER appuser

EXPOSE 8081

ENTRYPOINT ["./user-service"]
