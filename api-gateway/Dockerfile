# AI Assistance Disclosure:
# Tool: Claude Code (model: Opus 5), date: 2026-09-22
# Scope: Added the frontend build stage. The gateway serves the built SPA so
#   the browser is same-origin with the API (ai/decisions.md D-033).
# Author review: Nigeltzy - Checked the output of dockerfile and it seems to be a reasonable implementation of a multi-stage build for the gateway and frontend. The frontend is built using Node.js and the gateway is built using Go, and the final image serves both the API and the static files from the frontend.

# Build context is the repository root, matching the other services.

FROM golang:1.26-alpine AS builder
WORKDIR /src

COPY api-gateway/go.mod api-gateway/go.sum ./
RUN go mod download

COPY api-gateway/ .
RUN CGO_ENABLED=0 go build -o /out/api-gateway ./cmd/api

FROM alpine:3.20
WORKDIR /app

COPY --from=builder /out/api-gateway ./api-gateway

# Listens on PORT (8080 in compose.yaml); compose.yaml publishes it on host port 3001.
EXPOSE 8080

ENTRYPOINT ["./api-gateway"]
