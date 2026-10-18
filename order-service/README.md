# Order Service

The authority on the errand lifecycle: whether an errand exists, which state it
is in, and who its requester and courier are. An errand is in one of seven
states, `OPEN`, `ACCEPTED`, `PICKED_UP`, `DELIVERED`, `COMPLETED`, `CANCELLED`
and `EXPIRED`, and moves between them only through the actions under Routes. The
service owns three tables in its own PostgreSQL database, `orders`,
`order_history` and `outbox`.

It holds no credit balances. It reserves an errand's reward through
credit-service before saving the errand, adjusts the hold when the reward is
amended and publishes `errand.reward_set` so the hold always comes back in line
with the reward, and settles by publishing lifecycle events to RabbitMQ through
the outbox. Each settlement event (`errand.completed`, `errand.cancelled`,
`errand.expired`, `errand.failed`) carries in `amount` the errand's reward at
that transition, which is what settles; credit-service returns any hold left
over to the requester. It verifies every caller's access token itself. A
sweeper expires errands and auto-confirms deliveries every 5 seconds.

Owner: James Teo. Requirements: [`docs/requirements.md`](docs/requirements.md).
Decisions: the D-0xx rows in [`ai/decisions.md`](../ai/decisions.md). The HTTP
contract: [`api/openapi.yaml`](api/openapi.yaml), with its Go types and server
interface generated into `internal/gen/`.

## Run it

From `order-service/`, with PostgreSQL reachable at `ORDER_DB_URL`:

```bash
go run ./cmd/api                  # applies migrations/ at startup, then serves on PORT
go test ./...                     # unit tests, no Docker
go test -tags=integration ./...   # integration tests against PostgreSQL in testcontainers; needs Docker
```

The service reads its process environment and does not load a `.env` file;
`.env.example` lists every variable with a value for a host run. Every route
except `/health` needs an access token issued by user-service, which
order-service verifies itself against `JWKS_URL` and checks against the
revocation keys in `REDIS_URL`; `X-User-Id` and `X-User-Role` are ignored. To
call a host run directly, sign in through the gateway with the shared test
account (root `AGENTS.md` §11) and send its token:

```bash
TOKEN=$(curl -s -X POST http://localhost:8080/auth/login -H 'Content-Type: application/json' \
  -d '{"identifier":"student2","password":"Student123"}' | jq -r .accessToken)
curl -H "Authorization: Bearer $TOKEN" http://localhost:8083/locations
```

The host run must also reach user-service's keys at `JWKS_URL`, and Compose
publishes no port for user-service; until the keys can be fetched, a call with
a token answers 503.

From the repository root, with the root `.env` in place:

```bash
docker compose up --build order-service   # order-service and order-db
docker compose up --build                 # the whole stack, at http://localhost:8080/api/orders
```

Compose publishes no port for order-service, so from the host it is reachable
only through the gateway. The whole-stack tests are in
[`../e2e`](../e2e/README.md).

## Environment

| Variable | Default | In Compose | What it is |
| --- | --- | --- | --- |
| `PORT` | `8083` | `8083` | The HTTP port |
| `ORDER_DB_URL` | `postgres://postgres:postgres@localhost:5432/orders?sslmode=disable` | the `order-db` container | The service's own database |
| `SUPPLIER_BASE_URL` | `http://localhost:8082` | `http://supplier-service:8082` | Where suppliers are checked |
| `CREDIT_BASE_URL` | `http://localhost:8084` | `http://credit-service:8084` | Where rewards are reserved and adjusted |
| `RABBITMQ_URL` | `amqp://foc:foc-dev-password@localhost:5672/foc` | `amqp://foc:foc-dev-password@rabbitmq:5672/foc`; the shell and `.env` cannot override it | The broker the outbox relay publishes to, exchange `foc.events` |
| `JWKS_URL` | `http://localhost:8081/.well-known/jwks.json` | `http://user-service:8081/.well-known/jwks.json` | Where user-service publishes the keys access tokens are verified with |
| `REDIS_URL` | `redis://localhost:6379` | `redis://redis:6379` | The shared Redis holding user-service's revocation keys |
| `ORDER_EXPIRY_PERIOD` | `2h` | `${ORDER_EXPIRY_PERIOD:-2h}` | T: how long an errand stays `OPEN` |
| `ORDER_MAX_ACTIVE_ASSIGNMENTS` | `2` | `${ORDER_MAX_ACTIVE_ASSIGNMENTS:-2}` | K: how many errands a courier may hold in `ACCEPTED` or `PICKED_UP` |
| `ORDER_CONFIRMATION_WINDOW` | `24h` | `${ORDER_CONFIRMATION_WINDOW:-24h}` | C: how long a requester has to confirm before auto-confirm |

A variable that is unset takes its default. One that is set but malformed (a
duration that is not positive Go syntax such as `2h` or `30s`, a K below 1, a
`RABBITMQ_URL` or `REDIS_URL` that does not parse as an `amqp(s)://` or
`redis(s)://` URL, or a `JWKS_URL` that is not `http(s)://`) stops the service
at startup. Neither user-service nor Redis has to be up for it to start: the
keys are fetched on first use, and Redis is pinged once at startup, with an
error logged if it does not answer. While Redis cannot be reached the
revocation check is skipped and logged; while it answers with an error, such as
a missing or wrong password, every request with a token answers 503.

## Routes

The browser calls `/api/orders` plus the path below; the gateway strips that
prefix and forwards the access token. Every route except `/health` needs a valid
token and answers 401 without one, and 503 while user-service's keys cannot be
loaded or Redis answers the revocation lookup with an error. Errors are
`{"error": "<message>"}`.

| Method | Service path | Browser path | Who | What |
| --- | --- | --- | --- | --- |
| `GET` | `/health` | not for the browser | anyone | Liveness and database readiness |
| `POST` | `/` | `/api/orders` | a STUDENT with fewer than 5 errands `OPEN` | Post an errand in `OPEN`. A repeated `X-Request-Id` returns the errand it created with 200 and touches no credits |
| `GET` | `/?scope=active` | `/api/orders?scope=active` | ADMIN | Every errand that is not final, oldest state change first |
| `GET` | `/open` | `/api/orders/open` | anyone signed in | `OPEN` errands before their deadline, newest first |
| `GET` | `/requested?scope=active\|history` | `/api/orders/requested?scope=…` | anyone signed in | The caller's own errands |
| `GET` | `/assigned?scope=active\|history` | `/api/orders/assigned?scope=…` | anyone signed in | The caller's assignments; `history` includes releases by withdrawal |
| `GET` | `/locations` | `/api/orders/locations` | anyone signed in | The campus delivery locations |
| `GET` | `/suppliers/{supplierId}/active-errands` | not for the browser | ADMIN, through supplier-service's delete check | How many errands that are not final name a supplier |
| `GET` | `/{errandId}` | `/api/orders/{errandId}` | its requester, its courier, ADMIN; anyone while `OPEN` | One errand with its history |
| `PATCH` | `/{errandId}` | `/api/orders/{errandId}` | its requester | Amend an `OPEN` errand; send `version` and the fields to change |
| `POST` | `/{errandId}/accept` | `/api/orders/{errandId}/accept` | a STUDENT other than the requester who has not withdrawn from it | `OPEN` to `ACCEPTED`; send the `version` you were shown |
| `POST` | `/{errandId}/withdraw` | `/api/orders/{errandId}/withdraw` | its courier | `ACCEPTED` back to `OPEN`, with a fresh deadline |
| `POST` | `/{errandId}/pickup` | `/api/orders/{errandId}/pickup` | its courier | `ACCEPTED` to `PICKED_UP` |
| `POST` | `/{errandId}/deliver` | `/api/orders/{errandId}/deliver` | its courier | `PICKED_UP` to `DELIVERED`; starts C |
| `POST` | `/{errandId}/confirm` | `/api/orders/{errandId}/confirm` | its requester | `DELIVERED` to `COMPLETED` |
| `POST` | `/{errandId}/cancel` | `/api/orders/{errandId}/cancel` | its requester while `OPEN`; ADMIN while not final | To `CANCELLED` |
| `POST` | `/{errandId}/complete` | `/api/orders/{errandId}/complete` | ADMIN | `PICKED_UP` or `DELIVERED` to `COMPLETED` |

The gateway forwards every path under `/api/orders` once the token checks out,
so a signed-in caller can also reach `/health` there, and an ADMIN the supplier
count; both only read.

Every errand in a response lists the `actions` the viewer may take now, and
every response that carries errands also carries `serverTime`.

## Dependencies

What order-service needs at run time, and what it does while each is down:

- **credit-service**: reserves and adjusts rewards, and consumes the events
  order-service publishes. While it is down, posting an errand or amending its
  reward answers 503 and saves nothing; a failed create leaves only its queued
  `errand.failed` refund in the outbox.
- **supplier-service**: checks the supplier on create and amend. While it is
  down, both answer 503.
- **user-service**: publishes the keys access tokens are verified with
  (`JWKS_URL`) and writes the revocation keys in Redis (`REDIS_URL`). The keys
  are fetched on first use; while Redis cannot be reached, the revocation check
  is skipped and logged, and while it answers with an error, requests answer 503.
- **RabbitMQ**: receives the events. Until it answers, they wait in the outbox.
