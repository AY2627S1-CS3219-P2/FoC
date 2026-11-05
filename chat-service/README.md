# Chat Service

Errand-scoped requester–courier real-time messaging service for Friend on Campus.

## Architecture

- **Database:** PostgreSQL 16 (`chat-db`), owning `conversations`, `chat_messages`, `chat_audit_log`.
- **WebSocket:** Go `gorilla/websocket` with Redis pub/sub (`chat-redis`) for multi-replica horizontal scaling.
- **Handshake Tickets:** WebSocket connects via ephemeral single-use 30s tickets (`POST /api/chat/ws-ticket` -> `GET /api/chat/ws?ticket=...`) preventing token leak in access logs.
- **Lifecycle Events:** Listens to `chat.errands` on RabbitMQ for order state transitions (`errand.accepted`, `errand.withdrawn`, `errand.completed`, `errand.cancelled`, `errand.expired`).
- **Security:** Zero-trust local JWT validation against `user-service` JWKS endpoint + shared Redis suspension blocklist checks.

## Development

```bash
# Run unit tests
go test ./...

# Run integration tests (testcontainers-go with Docker)
go test -v -tags=integration ./...
```
