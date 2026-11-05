# Guidelines for chat-service

- Zero-trust authentication: Always verify Bearer JWT locally via `JWKS_URL` and query `AUTH_REDIS_URL` for revocation/suspension. Never trust identity headers from reverse proxy.
- Use composite cursor `(created_at, id)` for deterministic message pagination.
- Enforce strict Unicode rune count $\le 250$ for text messages via `utf8.RuneCountInString`.
- Enforce $\le 10$ MB file limit and JPEG/PNG MIME verification using magic byte inspection.
- Enforce idempotency: Duplicate `client_message_id` returns existing message with 200 OK.
