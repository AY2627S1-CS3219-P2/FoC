<!--
AI Assistance Disclosure:
Tool: Claude Code (model: Opus 5.5), date: 2026-11-05
Scope: Service rules, transcribed from the owner's decisions D-140 to D-146.
Author review: PENDING — <reviewer to complete>
-->

# Notification Service

Tells users about state changes on the errands they are party to (backlog F5).
It is an add-on: it reads events other services already publish, on its own
queue, and stores a list each user can read.

## Owner

Nigeltzy (D-140).

## Boundaries

- **Nothing depends on this service (D-140).** No service calls it, waits for it
  or fails because it is down. Events do not flow through it. Never add a call
  to it from another service, and never make its queue a step in a workflow.
- **It only reads events.** It publishes nothing and changes no order state or
  credit balance (F5.5.1).
- **Every notification comes from an order lifecycle event (F5.5).** It never
  calls order-service or credit-service to find anything out.
- **Queue `notification.errands` must not use `reject-publish` (D-143).** A full
  `reject-publish` queue nacks the publisher, and order-service's relay retries
  a nack, so an outage here would reach publishers.
  `internal/consumer/consumer_test.go` guards this.
- **Zero-trust auth (D-041, D-054).** Verify the bearer token locally and check
  the revocation keys. Never trust `X-User-Id` or `X-User-Role`.

## What each event does (D-141)

| Event | Requester | Courier |
| --- | --- | --- |
| `errand.accepted` | told (F5.2.1) | — |
| `errand.picked_up` | told (F5.2.2) | — |
| `errand.delivered` | told, with window C (F5.2.3) | — |
| `errand.expired` | told (F5.2.4) | — |
| `errand.withdrawn` | told (F5.2.5) | — |
| `errand.completed` | told | told their credits are being transferred (F5.3.2) |
| `errand.cancelled` | told | told, if one was assigned (F5.4) |

F5.3.1 has no notification: window P was never built (D-050), so no event says a
courier was released.

`errand.cancelled` names no courier, so `errand_couriers` remembers who is on each
errand from `errand.accepted` and `errand.withdrawn` (D-146). A cancel the service
sees before the `errand.accepted` before it, or an errand accepted before this
service existed, reaches the requester only.

## Layout

Module path `foc/notification-service`, layered as root §6 says.

- `internal/notification`: the rules, repository interface, errors.
- `internal/repository`: PostgreSQL adapter and migrations runner.
- `internal/consumer`: RabbitMQ consumer on `notification.errands`.
- `internal/httpapi`: handlers, router, token checks.
- `internal/auth`: copied from `order-service`, on purpose (root §4 rule 2).
- `internal/gen`: generated from `api/openapi.yaml`. Regenerate with
  `go generate ./internal/gen`; never edit it by hand.

## Commands

From this folder: `go run ./cmd/api`, `go test ./...`, and with Docker running
`go test -tags=integration ./...` (on Windows, set
`TESTCONTAINERS_RYUK_DISABLED=true`).
