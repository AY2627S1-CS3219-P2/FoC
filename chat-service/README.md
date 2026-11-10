<!--
AI Assistance Disclosure:
Tool: Antigravity, date: 2026-11-10
Scope: Expanded and enriched chat-service README with detailed architecture, invariants, database schema, WebSocket ticket protocol, API surface, RabbitMQ event lifecycle, configuration, and testing guides.
Author review: Reviewed by Seth
-->

# Chat Service

Errand-scoped real-time messaging service for Friend on Campus (FoC). It manages secure, 1:1 communications between requesters and couriers, handling conversation lifecycles, message logs, media attachments, unread notifications, and WebSocket streaming.

## Architecture & Invariants

1. **Errand-Scoped 1:1 Lifecycle**:
   - Conversations are strictly tied to errands. A conversation is automatically provisioned when a courier accepts an errand (`errand.accepted`).
   - If a courier withdraws (`errand.withdrawn`), that courier's conversation is immediately frozen and becomes permanently read-only for them.
   - When an errand completes (`errand.completed`), a 24-hour countdown starts, after which the background sweeper freezes the conversation permanently.
   - Cancelled or expired errands (`errand.cancelled`, `errand.expired`) freeze conversations immediately.
   - Strangers are completely barred from conversations; only the assigned requester, assigned courier, or an administrator (audited) may view messages.

2. **Dual-Redis Isolation (D-054)**:
   - **Dedicated Redis (`CHAT_REDIS_URL`)**: Owns ephemeral single-use 30-second handshake tickets and horizontal WebSocket Pub/Sub broadcasting across replicas.
   - **Shared Auth Redis (`AUTH_REDIS_URL`)**: Read-only zero-trust verification checking user session validity and active suspension status (`suspension:<userId>`).

3. **Secure Ticket-Based WebSocket Handshake**:
   - WebSockets require an ephemeral, single-use ticket acquired via authenticated REST (`POST /api/chat/ws-ticket`).
   - The client passes this ticket as a query parameter when establishing the WebSocket connection (`GET /api/chat/ws?ticket=<uuid>`).
   - Prevents exposing sensitive Bearer JWT tokens in query parameters, access logs, or proxy traces.

4. **Single-Step Media Atomicity**:
   - Images (JPEG/PNG up to 10 MB) are uploaded via multipart form data (`POST /api/chat/conversations/{id}/messages`).
   - If the database insertion or transaction fails, written files on disk are immediately purged, guaranteeing zero orphaned files on disk.

5. **Role-Scoped Unread Tracking**:
   - Each participant maintains an independent `last_read_at` receipt timestamp.
   - Supports role-scoped badges (`requester_has_unread` and `courier_has_unread`) so notifications appear only when switching into the relevant persona mode.

6. **Automated 24h Expiration Sweeper**:
   - A background sweeper runs every 60 seconds (`internal/sweeper`) to find and mark completed conversations whose 24-hour grace window has elapsed as frozen.

---

## Database Schema (PostgreSQL 16)

The service manages 4 core tables in `chat-db`:

- `conversations`: Primary conversation entity mapped to `errand_id`, `requester_id`, and `courier_id`. Tracks lifecycle status (`ACTIVE`, `FROZEN`, `ARCHIVED`), `created_at`, `expires_at`, and `closed_at`. Includes dynamic `read_only` response calculation.
- `chat_messages`: Append-only message log containing `id`, `conversation_id`, `sender_id`, `sender_role`, `content`, `media_url`, and `created_at`.
- `chat_read_receipts`: Participant read pointers storing `(conversation_id, user_id, last_read_at)` used to compute unread counts and synchronization.
- `chat_audit_log`: Immutable audit records capturing every administrative inspection of user conversations for compliance and dispute mediation.

---

## HTTP & WebSocket API Surface

All routes are mounted behind the API Gateway at `/api/chat/*`.

### REST Endpoints
| Method | Path | Auth | Description |
| :--- | :--- | :--- | :--- |
| `POST` | `/api/chat/ws-ticket` | Bearer JWT | Generates a single-use, 30-second ticket for WebSocket upgrade. |
| `GET` | `/api/chat/conversations` | Bearer JWT | Lists conversations for the authenticated user; supports optional `?errandId={uuid}` filter. |
| `GET` | `/api/chat/conversations/{id}` | Bearer JWT | Retrieves conversation metadata and participants. |
| `GET` | `/api/chat/conversations/{id}/messages` | Bearer JWT | Returns chronological message history for an authorized conversation. |
| `POST` | `/api/chat/conversations/{id}/messages` | Bearer JWT | Sends a text message or multipart image attachment (`file` field). |
| `POST` | `/api/chat/conversations/{id}/read` | Bearer JWT | Updates the participant's `last_read_at` pointer. |
| `GET` | `/api/chat/unread-summary` | Bearer JWT | Retrieves active unread message counts split by requester and courier roles. |
| `GET` | `/api/chat/images/{imageId}` | Bearer JWT | Securely streams uploaded media to authorized conversation participants. |
| `GET` | `/api/chat/admin/conversations` | Admin JWT | Admin endpoint to inspect conversations; automatically logs an entry in `chat_audit_log`. |
| `GET` | `/health` | None | Service liveness and dependency health check. |

### Real-Time WebSocket Streaming
- **Connection**: `GET /api/chat/ws?ticket={ticket_uuid}` (forwarded through the gateway at `GET /api/chat/ws`).
- **Events**:
  - `message`: Inbound/outbound chat message broadcast.
  - `read_receipt`: Read synchronization event updating message read state across open clients.
  - `typing`: Real-time user typing indicators.

---

## Asynchronous Messaging (RabbitMQ)

The service consumes order lifecycle events from topic exchange `foc.events` via durable queue `chat.errands`:

| Event Routing Key | Payload | Effect on Chat Service |
| :--- | :--- | :--- |
| `errand.accepted` | `errandId`, `requesterId`, `courierId` | Provisions a new active conversation between requester and courier. |
| `errand.withdrawn` | `errandId`, `requesterId`, `courierId` | Immediately freezes that courier's conversation, preserving read-only access. |
| `errand.completed` | `errandId` | Initiates the 24-hour expiration window before conversation is permanently frozen. |
| `errand.cancelled` | `errandId` | Immediately marks the conversation as frozen. |
| `errand.expired` | `errandId` | Immediately marks the conversation as frozen. |

---

## Environment Configuration

| Variable | Default | Description |
| :--- | :--- | :--- |
| `PORT` | `8085` | HTTP and WebSocket server listening port. |
| `CHAT_DB_URL` | *Required* | PostgreSQL connection string (`chat-db:5432/chat`). |
| `CHAT_REDIS_URL` | *Required* | Dedicated Redis connection for tickets and WebSocket Pub/Sub. |
| `AUTH_REDIS_URL` | `REDIS_URL` | Shared Redis connection for JWT revocation & suspension checks. |
| `JWKS_URL` | *Required* | User service public key URL (`http://user-service:8081/.well-known/jwks.json`). |
| `RABBITMQ_URL` | *Required* | AMQP broker connection string (`amqp://...`). |
| `MEDIA_STORAGE_PATH` | `/var/lib/foc/chat-media` | Filesystem directory storing uploaded chat media. |
| `MIGRATIONS_DIR` | `migrations` | Directory containing SQL schema migration files. |

---

## Development & Testing

### Running Tests Locally

```bash
# Run unit tests (no Docker required)
go test ./...

# Run integration tests against real PostgreSQL and Redis (via testcontainers-go; requires Docker)
go test -v -tags=integration ./...
```

### Running with Docker Compose

To start `chat-service` alongside its database, Redis, and dependencies:

```bash
# Start chat-service, chat-db, and chat-redis
docker compose up --build chat-service

# Run entire stack including frontend and gateway
docker compose up --build
```
