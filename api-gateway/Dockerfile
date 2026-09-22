# AI Assistance Disclosure:
# Tool: Claude Code (model: Opus 5), date: 2026-09-22
# Scope: Added the frontend build stage. The gateway serves the built SPA so
#   the browser is same-origin with the API (ai/decisions.md D-033).
# Author review: Nigeltzy - Checked the output of dockerfile and it seems to be a reasonable implementation of a multi-stage build for the gateway and frontend. The frontend is built using Node.js and the gateway is built using Go, and the final image serves both the API and the static files from the frontend.

# Build context is the repository root, matching the other services.

# ---- The browser app -------------------------------------------------------
# Built here rather than in its own image because the gateway serves it. No
# VITE_ build arg for the API's location any more: the bundle uses relative
# paths, since the page and the API are one origin (D-033).
FROM node:22-alpine AS frontend
WORKDIR /app
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci
COPY frontend/tsconfig*.json frontend/vite.config.ts frontend/index.html ./
COPY frontend/src ./src
RUN npm run build

# ---- The gateway -----------------------------------------------------------
FROM golang:1.26-alpine AS builder
WORKDIR /src

COPY api-gateway/go.mod api-gateway/go.sum ./
RUN go mod download

COPY api-gateway/ .
RUN CGO_ENABLED=0 go build -o /out/api-gateway ./cmd/api

FROM alpine:3.20
WORKDIR /app

COPY --from=builder /out/api-gateway ./api-gateway
COPY --from=frontend /app/dist ./web

# Provisional port (D-018). Compose publishes it as 3001, the frontend's
# allocation in root AGENTS.md §3, because this is now the one thing the
# browser talks to.
EXPOSE 8080

ENTRYPOINT ["./api-gateway"]
