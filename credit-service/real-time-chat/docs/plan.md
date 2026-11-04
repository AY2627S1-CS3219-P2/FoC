# Implementation Plan & File-by-File Blueprint

```
FoC Repository
├── order-service/               # Emits errand.accepted & errand.withdrawn
├── rabbitmq/                    # Queue & topic bindings for chat lifecycle
├── api-gateway/                 # Routes /api/chat & supports WebSocket upgrades
├── compose.yaml & .env.example  # Container infrastructure: chat-service, chat-db, chat-redis, media volume
├── chat-service/                # [NEW MICROSERVICE] Core chat domain, DB, media, WS hub, dedicated Redis
└── frontend/                    # [NEW UI] Root WebSocket provider, chat drawer/modal, unread badges
```

---

## Phase 1: Event Infrastructure & Order Service Additions

To establish the chat lifecycle, `order-service` must publish events when a courier accepts an errand and when a courier withdraws.

### Files Changed in `order-service/`

#### 1. `order-service/internal/order/events.go`
* **Changes:** Add event routing constants:
  ```go
  RoutingAccepted  = "errand.accepted"
  RoutingWithdrawn = "errand.withdrawn"
  ```
  Ensure `EventPayload` includes `RequesterID` and `CourierID` for both routing keys.
* **Why:** The design document states: *"Courier accepts an errand $\rightarrow$ A chat is created"* and *"Courier withdraws $\rightarrow$ That courier’s chat becomes read-only"*. `chat-service` cannot create or freeze conversations without these domain events.

#### 2. `order-service/internal/order/actions.go`
* **Changes:**
  * In `Accept(...)`, construct an outbox event with `RoutingAccepted`, containing `errandId`, `requesterId`, and `courierId`. Pass it into `AcceptCommand`.
  * In `Withdraw(...)`, construct an outbox event with `RoutingWithdrawn`, containing `errandId`, `requesterId`, and the withdrawing `courierId`. Attach it to the `Transition.Event`.
* **Why:** In FoC's transactional outbox pattern, state mutations and outbox records commit in the same database transaction, ensuring no events are lost if the process crashes.

#### 3. `order-service/internal/repository/writes.go`
* **Changes:** Update `Accept(ctx context.Context, c order.AcceptCommand)` to call `insertEvent(ctx, tx, *c.Event)` inside the database transaction when `c.Event != nil`.
* **Why:** Guarantees that saving the courier assignment and recording the outbox event for RabbitMQ relay occur atomically.

#### 4. `order-service/internal/order/ports.go`
* **Changes:** Add `Event *OutboxEvent` field to `AcceptCommand` struct.
* **Why:** Thread the outbox event payload from the domain service to the repository implementation.

#### 5. `order-service/internal/order/ordertest/fakes.go` & `order-service/internal/order/actions_test.go`
* **Changes:** Update in-memory fake repository and unit tests to verify that `Accept` and `Withdraw` stage their respective outbox events.
* **Why:** Adheres to course testing standards (unit test coverage for all new state transitions).

---

## Phase 2: Broker Configuration & Infrastructure

### Files Changed in Root & Broker

#### 6. `rabbitmq/definitions.json`
* **Changes:**
  * Add quorum queue: `chat.errands` with dead-letter exchange `foc.dlx` and routing key `chat.errands.failed`.
  * Add dead-letter queue: `chat.errands.dlq`.
  * Add bindings from `foc.events` exchange to `chat.errands` for:
    * `errand.accepted`
    * `errand.withdrawn`
    * `errand.completed`
    * `errand.cancelled`
    * `errand.expired`
* **Why:**
  * Follows the decoupled queue naming convention (`<consumer_service>.<entity_plural>`) established across the broker.
  * `errand.failed` is intentionally **omitted** because in FoC it is solely an uncommitted pre-creation credit refund guard; an accepted errand with a chat never emits `errand.failed`.
  * `chat-service` reliably receives the 5 relevant lifecycle triggers even across broker restarts. Declaring quorum queues in `definitions.json` ensures RabbitMQ boots with the exact topology on `docker compose up`.

#### 7. `compose.yaml`
* **Changes:**
  * Add `chat-db` service: `postgres:16-alpine`, port internal, healthcheck `pg_isready`.
  * Add `chat-redis` service: `redis:7-alpine`, 100% owned by `chat-service` for tickets and WebSocket pub/sub. Port internal, healthcheck `redis-cli ping`.
  * Add `chat-service` service:
    * Build context `./chat-service`.
    * Environment:
      * `PORT=8085`
      * `CHAT_DB_URL=postgres://${CHAT_DB_USER:-chat}:${CHAT_DB_PASSWORD:-chat}@chat-db:5432/chat?sslmode=disable`
      * `CHAT_REDIS_URL=redis://chat-redis:6379` (own dedicated Redis for tickets and pub/sub)
      * `AUTH_REDIS_URL=redis://redis:6379` (shared Redis, READ-ONLY, for D-054 revocation checks)
      * `JWKS_URL=http://user-service:8081/.well-known/jwks.json`
      * `RABBITMQ_URL=amqp://foc:foc-dev-password@rabbitmq:5672/foc`
      * `MEDIA_STORAGE_PATH=/var/lib/foc/chat-media`
    * Volumes: `chat-media-data:/var/lib/foc/chat-media`.
  * Add named volumes: `chat-db-data`, `chat-redis-data`, and `chat-media-data`.
* **Why:** Integrates `chat-service` and its dedicated `chat-redis` into the local Docker ecosystem while adhering to D-010 (no public ports exposed to host except `api-gateway`). Maintains D-054 boundaries by keeping the shared `redis` strictly read-only for auth revocation checks.

#### 8. `.env.example` & `.env`
* **Changes:** Add placeholders for `CHAT_SRV_DB_USER`, `CHAT_SRV_DB_PW`, `CHAT_BASE_URL=http://chat-service:8085`.
* **Why:** AGENTS.md §9 mandates that every new environment variable must be recorded in `.env.example`.

---

## Phase 3: API Gateway Updates

### Files Changed in `api-gateway/`

#### 9. `api-gateway/internal/config/config.go`
* **Changes:** Add `Chat string` field to `Downstream` struct, read from `CHAT_BASE_URL`.
* **Why:** Injects downstream routing target without hardcoding internal URLs.

#### 10. `api-gateway/internal/proxy/proxy.go`
* **Changes:** Configure `httputil.ReverseProxy` to allow WebSocket protocol upgrade (`101 Switching Protocols`) for streaming routes:
  * For WebSocket upgrade requests (`Upgrade: websocket`), bypass the restrictive `ResponseHeaderTimeout: 15 * time.Second` transport so long-lived streaming connections are not prematurely terminated.
  * Preserve client-to-upstream raw `Authorization` header forwarding per D-041 (using `NewRetainingToken`).
* **Why:** Go's standard `http.Transport` with `ResponseHeaderTimeout` will close WebSockets after 15s. Customizing transport behavior on connection upgrade allows persistent full-duplex TCP streaming through the gateway.

#### 11. `api-gateway/internal/httpapi/router.go`
* **Changes:** Add `/api/chat` route mapping to `serviceRoutes`:
  ```go
  {"/api/chat", downstream.Chat, "", proxy.NewRetainingToken}
  ```
* **Why:** Forwards all `/api/chat/*` requests (REST and WebSocket upgrades) to `chat-service:8085`. Retains raw `Authorization` headers so `chat-service` can perform local zero-trust JWT verification.

---

## Phase 4: `chat-service` (New Microservice)

Create the directory `chat-service/` following the layering conventions from `supplier-service` and `order-service`.

### Scaffolding & Configuration Files

#### 12. `chat-service/go.mod` & `chat-service/go.sum`
* **Dependencies:** `github.com/go-chi/chi/v5`, `github.com/jackc/pgx/v5`, `github.com/google/uuid`, `github.com/rabbitmq/amqp091-go`, `github.com/redis/go-redis/v9`, `github.com/gorilla/websocket`, `github.com/lestrrat-go/jwx/v2`, `github.com/stretchr/testify`, `github.com/testcontainers/testcontainers-go`.
* **Why:** Go module definition declaring the exact dependencies for routing, database, messaging, WebSockets, and local JWKS token verification.

#### 13. `chat-service/Dockerfile` & `chat-service/.dockerignore`
* **Content:** Multi-stage Go build (Alpine runner) matching `order-service/Dockerfile`.
* **Why:** Enables containerized execution inside Docker Compose.

#### 14. `chat-service/AGENTS.md` & `chat-service/CLAUDE.md` & `chat-service/README.md`
* **Content:** Service ownership documentation, domain rules, table definitions, environment variables, and ports.
* **Why:** Adheres to repo documentation standards (single source of truth per service).

#### 15. `chat-service/api/openapi.yaml`
* **Endpoints:**
  * `POST /ws-ticket`: Authenticated endpoint (Bearer JWT) generating a 30s single-use ticket stored in `chat-redis`. Returns `{"ticket": "<uuid>"}`.
  * `GET /conversations?errandId={errandId}`: List conversations for caller (can filter by errand).
  * `GET /conversations/{conversationId}`: Get conversation details, status, and read status.
  * `GET /conversations/{conversationId}/messages`: Paginated message history using composite cursor `(created_at, id)`, with optional `since=<timestamp>` for gapless sync on reconnect.
  * `POST /conversations/{conversationId}/messages`: Send message endpoint:
    * Supports `application/json` for text messages (max 250 characters validated via `utf8.RuneCountInString`).
    * Supports `multipart/form-data` for atomic image uploads ($\le 10$ MB, JPEG/PNG).
    * Idempotent: If `client_message_id` already exists, returns existing message with `200 OK` (not `409 Conflict`).
  * `GET /images/{imageId}`: Download image bytes (access-controlled).
  * `POST /conversations/{conversationId}/read`: Mark conversation as read (updates caller's `last_read_at` on conversation).
  * `GET /unread-summary`: Returns whether the current user has unread messages and lists `unread_errand_ids` (powers global & per-card red dots).
  * `GET /ws`: WebSocket connection upgrade endpoint (authenticates via `?ticket=<ticket>`).
  * `GET /admin/conversations`: Admin-only view for all chats (read-only audit, logs `chat.inspect` per D-106).
* **Why:** Spec-first contract requirement ([AGENTS.md §8](file:///Users/sethtay/Downloads/FoC/AGENTS.md#L198-L214)). Single atomic message creation endpoint eliminates orphan image files.

---

### Database Migrations

#### 16. `chat-service/migrations/000001_create_chat_schema.up.sql` & `down.sql`
* **Tables Created:**
  * `media_attachments`:
    * `id UUID PRIMARY KEY DEFAULT gen_random_uuid()`
    * `conversation_id UUID NOT NULL` (references `conversations(id)` on delete restrict)
    * `file_name TEXT NOT NULL`
    * `mime_type TEXT NOT NULL`
    * `file_size BIGINT NOT NULL`
    * `storage_path TEXT NOT NULL`
    * `created_at TIMESTAMPTZ NOT NULL DEFAULT now()`
  * `conversations`:
    * `id UUID PRIMARY KEY DEFAULT gen_random_uuid()`
    * `errand_id UUID NOT NULL`
    * `requester_id UUID NOT NULL`
    * `courier_id UUID NOT NULL`
    * `status VARCHAR(20) NOT NULL DEFAULT 'ACTIVE'` (`ACTIVE`, `FROZEN`)
    * `errand_completed_at TIMESTAMPTZ`
    * `last_event_occurred_at TIMESTAMPTZ NOT NULL DEFAULT now()`
    * `requester_last_read_at TIMESTAMPTZ NOT NULL DEFAULT now()`
    * `courier_last_read_at TIMESTAMPTZ NOT NULL DEFAULT now()`
    * `created_at TIMESTAMPTZ NOT NULL DEFAULT now()`
    * `updated_at TIMESTAMPTZ NOT NULL DEFAULT now()`
    * `CONSTRAINT uq_errand_courier UNIQUE (errand_id, courier_id)`
  * `messages`:
    * `id UUID PRIMARY KEY DEFAULT gen_random_uuid()`
    * `conversation_id UUID NOT NULL REFERENCES conversations(id) ON DELETE RESTRICT`
    * `sender_id UUID NOT NULL`
    * `client_message_id TEXT` (idempotency key)
    * `message_type VARCHAR(10) NOT NULL` (`TEXT` or `IMAGE`)
    * `content VARCHAR(250)` (nullable if image)
    * `media_id UUID REFERENCES media_attachments(id) ON DELETE RESTRICT` (nullable if text)
    * `created_at TIMESTAMPTZ NOT NULL DEFAULT now()`
    * `CONSTRAINT uq_conv_client_msg UNIQUE (conversation_id, client_message_id)`
  * `chat_audit_log`:
    * `id BIGSERIAL PRIMARY KEY`
    * `actor_id UUID NOT NULL`
    * `action VARCHAR(50) NOT NULL` (`chat.inspect`)
    * `conversation_id UUID REFERENCES conversations(id) ON DELETE SET NULL`
    * `occurred_at TIMESTAMPTZ NOT NULL DEFAULT now()`
  * **Indexes:**
    * Partial unique index guaranteeing only one active courier conversation per errand:
      ```sql
      CREATE UNIQUE INDEX uq_active_conv_per_errand ON conversations (errand_id) WHERE status = 'ACTIVE';
      ```
    * Indexes on `(errand_id)`, `(requester_id)`, `(courier_id)`.
    * Composite cursor index for deterministic pagination:
      ```sql
      CREATE INDEX idx_messages_cursor ON messages (conversation_id, created_at DESC, id DESC);
      ```
* **Why:**
  * Implements the core domain schema, media attachments, and idempotency guarantees.
  * Replaces ambiguous boolean flags with a single domain `status` (`ACTIVE` vs `FROZEN`).
  * `last_event_occurred_at` ensures that out-of-order RabbitMQ messages (e.g. `withdrawn` arriving before `accepted`) are safely discarded or applied without state corruption.
  * Implements D-106 audit compliance for admin chat inspections.
  * Storing read timestamps directly on `conversations` provides an efficient, constant-time $O(1)$ single-row update whenever a user opens a conversation:
    ```sql
    UPDATE conversations SET requester_last_read_at = now() WHERE id = $1;
    ```
  * Checking unread status across all user conversations returns both boolean `has_unread` and `unread_errand_ids` in a single index scan.

---

### Domain Layer (`chat-service/internal/chat/`)

#### 17. `chat-service/internal/chat/model.go`
* **Structs:** `Conversation`, `ConversationStatus` (`StatusActive`, `StatusFrozen`), `Message`, `MessageType`, `MediaAttachment`, `UnreadSummary`, `Caller` (UserID, Role).
  * `Conversation` holds `Status`, `ErrandCompletedAt`, `LastEventOccurredAt`, `RequesterLastReadAt`, and `CourierLastReadAt`.
  * `UnreadSummary` holds `HasUnread bool` and `UnreadErrandIDs []uuid.UUID`.
* **Why:** Core domain entities independent of transport (HTTP) and storage (SQL).

#### 18. `chat-service/internal/chat/errors.go`
* **Sentinels:** `ErrNotFound`, `ErrUnauthorized`, `ErrConversationReadOnly`, `ErrMessageTooLong` (> 250 chars), `ErrInvalidImageType` (not jpg/png), `ErrImageTooLarge` (> 10MB), `ErrUserSuspended`, `ErrInvalidTicket`.
* **Why:** Standardized domain errors mapped to HTTP status codes in transport layer.

#### 19. `chat-service/internal/chat/rules.go`
* **Functions:**
  * `ValidateMessageLength(text string) error`: Validates `utf8.RuneCountInString(text) <= 250` (prevents false-positive byte overflow on emojis and smart punctuation).
  * `IsReadOnly(conv Conversation, now time.Time) bool`: Returns `true` if `conv.Status == StatusFrozen` OR `conv.ErrandCompletedAt != nil && now.After(conv.ErrandCompletedAt.Add(24*time.Hour))`.
  * `CanAccess(conv Conversation, caller Caller) bool`: Requesters can see their own errands; couriers can see errands they accepted (including withdrawn); admins have read-only access to all; strangers get rejected.
  * `CanSendMessage(conv Conversation, caller Caller, now time.Time) error`: Checks read-only status and verifies caller is either the requester or the active courier.
* **Why:** Encapsulates the core business rules specified in Sections 2, 3, and 4 of the design document.

#### 20. `chat-service/internal/chat/ports.go`
* **Interfaces:**
  * `Repository`: Interface for conversation, message, idempotency queries, audit logging, `UpdateLastReadAt(...)`, and `GetUnreadSummary(...)`.
  * `TicketStore`: Interface for issuing and redeeming 30s single-use handshake tickets (`IssueTicket`, `RedeemTicket`).
  * `MediaStorage`: Interface for saving, reading, and cleaning up raw image bytes (`Save`, `Get`, `Delete`).
  * `EventPublisher`: Interface for emitting real-time notifications to connected clients.
* **Why:** Enforces Hexagonal / Ports & Adapters architecture. Enables fast unit testing with in-memory fakes.

#### 21. `chat-service/internal/chat/service.go`
* **Methods:**
  * `IssueWSTicket(ctx, caller)`: Generates 30s ticket in `chat-redis`.
  * `RedeemWSTicket(ctx, ticket)`: Validates and consumes ticket atomically.
  * `CreateOrResumeConversation(ctx, errandId, requesterId, courierId, occurredAt)`: Applies only if `occurredAt >= last_event_occurred_at`.
  * `MarkCourierWithdrawn(ctx, errandId, courierId, occurredAt)`: Freezes conversation (`status = 'FROZEN'`).
  * `MarkErrandCompleted(ctx, errandId, completedAt, occurredAt)`: Records completion time and begins 24-hour window.
  * `MarkErrandTerminated(ctx, errandId, occurredAt)`: Freezes all conversations for `errand_id` (`status = 'FROZEN'`), cleanly handling admin cancellations where `courierId` is null.
  * `FreezeExpiredCompletedConversations(ctx)`: Background sweeper method that sets `status = 'FROZEN'` on conversations where `now > errand_completed_at + 24 hours` and broadcasts `conversation.read_only`.
  * `SendMessage(ctx, caller, convId, clientMsgId, text)`: Idempotently creates or returns existing text message.
  * `SendImage(ctx, caller, convId, clientMsgId, fileName, mimeType, reader)`: Atomic 1-step image save + message record. Deletes file on disk if DB insert fails.
  * `MarkAsRead(ctx, caller, convId)`: Updates `requester_last_read_at` or `courier_last_read_at` on `conversations`.
  * `GetUnreadSummary(ctx, caller)`: Queries if any unread messages exist for the user and returns unread errand IDs.
  * `RecordAdminInspection(ctx, adminID, convID)`: Writes audit row per D-106.
* **Why:** Coordinates database writes, media uploads, and real-time broadcasts with monotonic event ordering.

---

### Media Storage Layer (`chat-service/internal/media/`)

#### 22. `chat-service/internal/media/storage.go`
* **Implementation:** `LocalStorage` implementing `chat.MediaStorage`.
  * Writes to directory specified by `MEDIA_STORAGE_PATH` (e.g. `/var/lib/foc/chat-media`).
  * Validates file size $\le 10$ MB.
  * Sniffs MIME type via `http.DetectContentType` to enforce `image/jpeg` or `image/png`.
  * Provides `Delete(storagePath)` helper so failed message transactions immediately clean up disk files.
* **Why:** Fulfills Requirement 3 (store locally for dev) while keeping the interface cleanly decoupled so an `S3Storage` adapter can be dropped in when deploying to AWS without touching domain logic.

---

### Real-Time Hub & Redis Layer (`chat-service/internal/hub/`)

#### 23. `chat-service/internal/hub/hub.go` & `client.go`
* **Components:**
  * `Hub`: Thread-safe registry of connected user WebSockets (`map[uuid.UUID]map[*Client]bool`). Connected to `chat-redis` Pub/Sub channels (`chat:events:user:<userId>`) for horizontal multi-instance scaling.
  * `Client`: Reads from/writes to `*websocket.Conn` with read/write pumps, 30s heartbeats (`Ping/Pong`), and backpressure buffering.
  * **Mid-session Token Refresh:** Listens for `{"type":"auth_refresh", "token":"<new_jwt>"}` frames to extend connection validity without disconnecting active users.
  * **Suspension Enforcement:** On each 30s Ping/Pong heartbeat, verifies caller against shared Redis (`AUTH_REDIS_URL`) for `suspension:<userId>`. Immediately terminates connection if suspended.
  * `Broadcast(userId uuid.UUID, event RealtimeEvent)`: Publishes event to `chat-redis` channel so all instances push to the target user's active sockets.
* **Why:** Ensures seamless real-time delivery across instances without leaking tokens or disconnecting active users.

---

### Background Sweeper Layer (`chat-service/internal/sweeper/`)

#### 24. `chat-service/internal/sweeper/expiry_sweeper.go`
* **Worker:** Periodic background ticker (running every 60s) calling `service.FreezeExpiredCompletedConversations(ctx)`.
* **Action:** Queries for conversations where `status = 'ACTIVE' AND errand_completed_at IS NOT NULL AND now() > errand_completed_at + 24 hours`.
* **Effect:** Updates `status = 'FROZEN'` and broadcasts `conversation.read_only` over WebSocket to active participants.
* **Why:** Guarantees that conversations cleanly transition to read-only even if neither user initiates a new message.

---

### Consumer Layer (`chat-service/internal/consumer/`)

#### 25. `chat-service/internal/consumer/amqp.go`
* **Consumer:** Subscribes to RabbitMQ queue `chat.errands`.
* **Message Dispatcher:**
  * On `errand.accepted`: Calls `service.CreateOrResumeConversation(..., event.OccurredAt)`.
  * On `errand.withdrawn`: Calls `service.MarkCourierWithdrawn(..., event.OccurredAt)`.
  * On `errand.completed`: Calls `service.MarkErrandCompleted(..., event.OccurredAt)`.
  * On `errand.cancelled` / `errand.expired`: Calls `service.MarkErrandTerminated(event.ErrandID, event.OccurredAt)`.
* **Why:** Asynchronously synchronizes the chat lifecycle with order state machine without tight coupling to `order-service`. Guarantees race-proof state application via `occurredAt`.

---

### Repository & Redis Adapter Layer (`chat-service/internal/repository/`)

#### 26. `chat-service/internal/repository/postgres.go`
* **Content:** PostgreSQL database connection, transaction helpers (`inTx`), and migration bootstrapper.
* **Why:** Encapsulates `pgx/v5` connection pool management.

#### 27. `chat-service/internal/repository/queries.go`
* **Content:** SQL queries for conversations, messages, and single-row updates to `requester_last_read_at` or `courier_last_read_at`. Implements cursor pagination and gapless sync:
  ```sql
  -- Standard cursor pagination
  SELECT id, conversation_id, sender_id, message_type, content, media_id, created_at
  FROM messages
  WHERE conversation_id = $1 AND (created_at, id) < ($cursor_time, $cursor_id)
  ORDER BY created_at DESC, id DESC
  LIMIT $limit;

  -- Gapless reconnect sync
  SELECT id, conversation_id, sender_id, message_type, content, media_id, created_at
  FROM messages
  WHERE conversation_id = $1 AND created_at > $since_timestamp
  ORDER BY created_at ASC, id ASC;
  ```
  **Idempotent Message Insert:**
  ```sql
  INSERT INTO messages (conversation_id, sender_id, client_message_id, message_type, content, media_id)
  VALUES ($1, $2, $3, $4, $5, $6)
  ON CONFLICT (conversation_id, client_message_id) DO NOTHING
  RETURNING id, conversation_id, sender_id, message_type, content, media_id, created_at;
  ```
  If no row returned by `ON CONFLICT DO NOTHING`, queries the existing message by `(conversation_id, client_message_id)` and returns it.
* **Why:** Isolates all SQL statements from domain logic, guarantees collision-free cursor pagination, gapless reconnect catchup, and true HTTP idempotency on retried sends.

#### 28. `chat-service/internal/repository/ticket_store.go`
* **Content:** Redis ticket store implementing `chat.TicketStore` using `chat-redis`.
  * `IssueTicket`: `SET chat:ticket:<uuid> <userId> EX 30`.
  * `RedeemTicket`: Atomic `GETDEL chat:ticket:<uuid>`.
* **Why:** Ephemeral handshake token storage with automatic 30s expiry.

---

### Transport & Zero-Trust Auth Layer (`chat-service/internal/httpapi/`)

#### 29. `chat-service/internal/httpapi/auth_middleware.go`
* **Content:** Local zero-trust JWT verifier implementing D-041:
  * Fetches and caches keys from `JWKS_URL`.
  * Verifies claims (`exp`, `sub`, `iss`, `aud`).
  * Checks shared Redis (`AUTH_REDIS_URL`) for token revocation and `suspension:<userId>` (D-054).
  * Injects verified `Caller` into request context.
* **Why:** Conforms to D-041 zero-trust requirements.

#### 30. `chat-service/internal/httpapi/router.go`
* **Content:** Chi router mounting routes from `openapi.yaml`, CORS, logging, recovery, and local auth middleware.
* **Why:** Serves the HTTP API behind the API Gateway.

#### 31. `chat-service/internal/httpapi/handlers.go`
* **Handlers:** Handlers for `POST /ws-ticket`, message pagination (with `since` param support), text and image message creation (`POST /conversations/{id}/messages`), image download, read state (`POST /conversations/{id}/read`), and `GET /unread-summary`. Admin endpoints log inspection to `chat_audit_log`.
* **Why:** Decodes incoming requests, calls domain service, and returns structured JSON responses or HTTP errors. Returns `200 OK` on duplicate `client_message_id` sends.

#### 32. `chat-service/internal/httpapi/ws.go`
* **Handler:** Handles `GET /ws?ticket=<ticket>`. Validates ticket via `ticketStore.RedeemTicket(ticket)`, upgrades to WebSocket, and registers connection into `hub.Hub`.
* **Why:** Establishes secure WebSocket connection without raw tokens in query strings.

#### 33. `chat-service/cmd/api/main.go`
* **Content:** Main entrypoint: reads environment variables, establishes DB pool, connects to `chat-redis` and `AUTH_REDIS_URL`, initializes RabbitMQ consumer, starts background expiry sweeper, boots HTTP/WS server, and listens for OS shutdown signals.
* **Why:** Wiring root for the microservice.

---

## Phase 5: Frontend React Implementation

### Files Created/Changed in `frontend/src/`

#### 34. `frontend/src/features/chat/chatTypes.ts`
* **TypeScript Types:** `Conversation`, `ChatMessage`, `MessageType` (`TEXT` | `IMAGE`), `ChatEvent`, `UnreadSummaryResponse` (`has_unread: boolean`, `unread_errand_ids: string[]`), `WSTicketResponse`.
* **Why:** Strict type safety matching the backend OpenAPI schema.

#### 35. `frontend/src/features/chat/chatApi.ts`
* **API Client:** Fetch wrappers for:
  * `fetchWSTicket()`: Calls `POST /api/chat/ws-ticket` to obtain single-use ticket.
  * `fetchConversation(conversationId)`
  * `fetchConversationsByErrand(errandId)`
  * `fetchMessages(conversationId, cursor, since)`
  * `sendMessage(conversationId, text, clientMessageId)`
  * `sendImage(conversationId, file, clientMessageId)`
  * `markConversationRead(conversationId)`
  * `fetchUnreadSummary()`
* **Why:** Centralized network interface for chat endpoints.

#### 36. `frontend/src/features/chat/ChatSocketContext.tsx`
* **React Context & Provider:** Mounted at the application root (`App.tsx`):
  * Maintains the persistent WebSocket connection across route changes while the user is authenticated.
  * Requests ticket via `fetchWSTicket()` and connects to `/api/chat/ws?ticket=<ticket>`.
  * Manages global unread state (`hasUnread` and `unreadErrandIds: Set<string>`).
  * On reconnect after network interruption, calls `fetchMessages` to backfill any missed messages for active chats.
  * Automatically sends `{"type":"auth_refresh", "token":"<new_jwt>"}` when access token rotates.
  * Exposes `useChatSocketContext()` hook to components across the entire app.
* **Why:** Keeps the real-time socket alive and notifications updated even when browsing outside the chat view.

#### 37. `frontend/src/features/chat/ChatBox.tsx`
* **React Component:**
  * Message list with auto-scroll to bottom.
  * Sender distinction (requester vs courier bubbles).
  * Inline image viewer.
  * Character counter with strict 250-character limit (counted via Unicode characters).
  * File picker restricted to `.jpg`, `.jpeg`, `.png` ($\le 10$ MB client check).
  * "Not sent" badge with "Retry" button on network failure.
  * **24-Hour Countdown Banner:** When `errand_completed_at` is set, displays countdown notice (e.g. *"Errand completed. Chat closes in 3 hours (10:00 AM)"*).
  * When countdown hits zero or `conversation.read_only` arrives, displays *"This conversation is read-only"* and disables message inputs.
* **Why:** Core user-facing chat interface conforming to Sections 2, 4, 5, and 6 of the design document.

#### 38. `frontend/src/features/chat/UnreadBadge.tsx`
* **React Component:** Simple red dot indicator without numbers (matching Section 6: *"A red dot appears when a new message arrives. No numerical unread count is displayed"*).
* **Why:** Reusable unread badge component for navigation bars and errand cards.

#### 39. `frontend/src/features/errands/ErrandDetail.tsx`
* **Changes:** Mount the `ChatBox` inside the errand detail view whenever an assigned courier exists or when past conversations exist.
* **Why:** Provides seamless in-context communication directly on the active errand screen.

#### 40. `frontend/src/features/errands/ErrandCard.tsx` & `frontend/src/components/NavBar.tsx` (or Header)
* **Changes:** Render `<UnreadBadge />` on top nav (checking `hasUnread`) and on errand cards (checking `unreadErrandIds.has(errand.id)`).
* **Why:** Informs users of new messages across the app in real time.

---

## Phase 6: Automated Testing & Verification

### Test Files

#### 41. `chat-service/internal/chat/service_test.go` & `rules_test.go`
* **Unit Tests:**
  * 250-character Unicode text limit validation (emojis, smart quotes, multi-byte runes).
  * Image format and size rejection (>10MB, non-jpg/png).
  * 24-hour post-completion read-only cutoff.
  * Background expiry sweeper freezing completed conversations.
  * Courier withdrawal read-only freeze.
  * Admin read-only access and audit log creation.
  * Message idempotency with duplicate `client_message_id`.
  * `last_read_at` updating and unread status calculation.
  * Handshake ticket generation and redemption.
* **Why:** Validates all core business rules without requiring external containers.

#### 42. `chat-service/internal/repository/postgres_integration_test.go`
* **Integration Tests:** Uses `testcontainers-go` to run real PostgreSQL queries and migrations.
* **Why:** Verifies constraint enforcement, unique indexes, composite cursor pagination, gapless sync, and ACID transactions against real Postgres.

#### 43. `e2e/chat_workflow_test.go` (or `e2e/cypress/e2e/chat.cy.ts`)
* **End-to-End Test:** Full flow:
  1. Student 1 creates errand.
  2. Student 2 accepts errand $\rightarrow$ chat becomes active.
  3. Student 1 and Student 2 exchange text and images over WebSocket in real time via ticket handshake.
  4. Student 2 withdraws $\rightarrow$ chat becomes read-only.
  5. Student 3 accepts $\rightarrow$ separate new chat is created.
  6. Admin signs in $\rightarrow$ views both chats in read-only mode (audit entry recorded).
* **Why:** Covers the end-to-end user journey across the full Compose stack.

---

## Summary Checklist of Deliverables

| Area | New Files | Modified Files |
|---|---|---|
| **`order-service`** | 0 | 5 files (`events.go`, `actions.go`, `writes.go`, `ports.go`, tests) |
| **`rabbitmq`** | 0 | 1 file (`definitions.json`) |
| **`api-gateway`** | 0 | 3 files (`config.go`, `proxy.go`, `router.go`) |
| **Docker / Env** | 0 | 2 files (`compose.yaml`, `.env.example`) |
| **`chat-service`** | 22 new files (migrations, domain, media, hub, sweeper, consumer, repo, HTTP/WS, auth verifier, ticket store, OpenAPI) | 0 |
| **`frontend`** | 6 new files (`chatTypes.ts`, `chatApi.ts`, `ChatSocketContext.tsx`, `ChatBox.tsx`, `UnreadBadge.tsx`) | 3 files (`ErrandDetail.tsx`, `ErrandCard.tsx`, nav/header) |
| **Tests** | 3 new test suites (`service_test.go`, `postgres_integration_test.go`, E2E test) | Updated order tests |

This complete roadmap serves as the exact blueprint for implementing the feature.
