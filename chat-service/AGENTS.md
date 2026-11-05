# Chat Service

The authority on errand-scoped, real-time communication between requesters and couriers.
It owns chat conversations, message logs, image uploads, unread message indicators,
and WebSocket delivery.

## Owner

Seth Tay. Architecture decisions and blueprints follow the technical review and
the master plan in `credit-service/real-time-chat/docs/plan.md`.

## Boundaries

- **Errands and assignments are not owned by this service.** Conversations are created
  or frozen solely in response to domain events (`errand.accepted`, `errand.withdrawn`,
  `errand.completed`, `errand.cancelled`, `errand.expired`) published to RabbitMQ
  queue `chat.errands`. It never mutates order states or queries order-db directly.
- **Zero-Trust Auth (D-041):** Never trust `X-User-Id` or `X-User-Role` headers from
  the gateway. Authenticate requests locally via `JWKS_URL` and verify revocation
  and account suspension (`suspension:<userId>`) via shared Redis (`AUTH_REDIS_URL`).
- **Dedicated Redis (`CHAT_REDIS_URL`):** Handshake tickets for WebSocket connections
  and multi-node Pub/Sub message distribution belong exclusively to `chat-service`'s
  dedicated Redis. The shared Redis is strictly read-only for auth revocation checks (D-054).
- **Single-Step Media Atomicity:** Images are uploaded via multipart `POST /conversations/{id}/messages`.
  If database insertion fails, any written files are removed from the filesystem immediately,
  guaranteeing zero orphan files.

## Layout

Module path: `foc/chat-service`.
- `internal/chat`: Domain models, validation rules, repository ports, and service logic.
- `internal/repository`: PostgreSQL implementation (`chat-db`).
- `internal/media`: Local filesystem disk storage.
- `internal/ticket`: Redis store for 30s single-use handshake tickets.
- `internal/hub`: Real-time WebSocket connection manager and pub/sub listener.
- `internal/sweeper`: Background 60s cron job freezing completed errands past 24h.
- `internal/consumer`: RabbitMQ consumer listening to `chat.errands`.
- `internal/auth`: Zero-trust JWT verifier and Redis suspension checker.
- `internal/httpapi`: REST API router, HTTP handlers, and WebSocket upgrade handler.
- `cmd/api`: Entrypoint wiring DB, Redis, RabbitMQ, and HTTP server.
