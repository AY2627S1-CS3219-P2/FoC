# CS3219 — Software Design and Architecture (AY2627 Sem 1)

## Friend on Campus (FoC)

**Friend on Campus (FoC)** is a peer-to-peer campus errand platform where
students can request items to be collected from stores or facilities on
campus, and other students can fulfil (and deliver) those requests. The
platform runs on a closed credit economy — credits cannot be bought,
withdrawn, or exchanged for money, and only circulate within the platform.

---

## Features

- **Accounts and authentication:** NUS email registration, OTP verification, login, refresh sessions, logout, and profile management.
- **Campus suppliers:** Browse and search suppliers, view their details, and manage them through admin controls.
- **Errands:** Post immediately or schedule for later, amend details and rewards, accept, withdraw, collect, deliver, and confirm completion.
- **Lifecycle automation:** Expire open errands and automatically confirm deliveries after the configured confirmation window.
- **Credits:** Initial allocations, reward reservations, courier payouts, refunds, transaction history, and ledger reconciliation.
- **Chat:** Errand-scoped requester–courier messaging with WebSocket updates, image attachments, unread tracking, and lifecycle-based access.
- **Administration:** Account and role management, supplier management, errand oversight, flagged credit account review, and audit logs.
- **Observability:** Centralised container logs, service health monitoring, and Grafana dashboards.

## System Overview

The React frontend calls a single API Gateway. The gateway forwards API traffic to five internal Go services:

| Component | Responsibility |
| --- | --- |
| `api-gateway` | Public API entry point, authentication checks, routing, and refresh-cookie handling |
| `user-service` | Accounts, authentication, profiles, roles, and sessions |
| `supplier-service` | Campus suppliers, pickup locations, and supplier administration |
| `order-service` | Errand lifecycle, scheduling, history, and lifecycle events |
| `credit-service` | Credit accounts, reward holds, settlement, ledger, and reconciliation |
| `chat-service` | Errand conversations, messages, media, and real-time delivery |

Each backend service owns its PostgreSQL database. Services communicate through REST APIs and RabbitMQ events. Redis supports authentication revocation; chat also uses its own Redis instance.

The gateway is the only application API published to the host by Compose. The frontend runs separately.

## Technology Stack

| Area | Technologies |
| --- | --- |
| Frontend | React 19, TypeScript, Vite |
| Backend | Go 1.26, chi |
| Persistence | PostgreSQL 16, pgx, golang-migrate |
| Messaging | RabbitMQ |
| Redis | Token revocation, temporary authentication state, and chat infrastructure |
| Testing | Go testing, testify, testcontainers-go, Vitest |
| Local runtime | Docker Compose |
| Observability | Grafana, Loki, Alloy, Prometheus |

## Getting Started

### Prerequisites

- Docker with Docker Compose
- Node.js and npm
- Python 3 with `cryptography` for generating local signing keys
- Go 1.26 or later for running backend tests or services outside Docker

### 1. Configure the backend

From the repository root:

```bash
cp .env.example .env
```

Fill in the initial admin settings in `.env`. For the shared local demo account:

```dotenv
INITIAL_ADMIN_EMAIL=admin@u.nus.edu
INITIAL_ADMIN_USERNAME=admindemo
INITIAL_ADMIN_PASSWORD=AdminDemo123
OTP_DELIVERY=log
```

The admin account is created on first startup when the database has no admin account.

Keep `.env` and private signing keys out of version control.

### 2. Generate local signing keys

```bash
python3 -m venv .venv
.venv/bin/python -m pip install cryptography
.venv/bin/python user-service/generate_jwks.py --out user-service/keys.json
```

Compose mounts this file into user-service. It is not included in the container image.

### 3. Start the backend

```bash
docker compose up --build --wait
```

The gateway is available at `http://localhost:8080`. Its health endpoint is:

```text
GET http://localhost:8080/healthz
```

### 4. Start the frontend

In a separate terminal:

```bash
cd frontend
cp .env.example .env
npm ci
npm run dev
```

Open **http://localhost:3001**.

Leave `VITE_GATEWAY_BASE_URL` empty and `VITE_USE_FIXTURES` unset for the full application. Vite forwards `/auth` and `/api` to the gateway, including chat WebSocket traffic.

### 5. Try the application

Sign in as `admindemo` using the local password above to access administration.

For a student account, register through the frontend and verify the email using the code shown by:

```bash
docker compose logs user-service
```

`OTP_DELIVERY=log` is intended for local development. SendGrid delivery is also supported; see `user-service/README.md`.

Use two student accounts to try the complete requester–courier workflow: post an errand, accept it, chat, mark pickup and delivery, then confirm completion and inspect the credit balances.

### Stop the backend

```bash
docker compose down
```

To stop the optional observability services too:

```bash
docker compose --profile observability down
```

Named volumes retain local data. Adding `-v` deletes those volumes, including databases and uploaded chat media.

## Testing

### Backend unit tests and checks

Run inside the backend service being changed:

```bash
go test ./...
go vet ./...
```

Each service is a separate Go module; repeat these commands for the relevant services.

### Backend integration tests

For services with integration tests, run inside that service directory with Docker available:

```bash
go test -tags=integration ./...
```

### Frontend

From `frontend/`:

```bash
npm test
npm run build
```

### Whole-stack tests

From the repository root, after completing local configuration:

```bash
ORDER_EXPIRY_PERIOD=20s ORDER_CONFIRMATION_WINDOW=20s \
  docker compose up --build --wait

go -C e2e test -tags=e2e -count=1 ./...
```

The suite exercises the gateway against real services, including account verification, errand settlement, scheduling, concurrent acceptance, refunds, token revocation, supplier administration, chat, and audit logs.

Keep `OTP_DELIVERY=log` for this suite. It reads verification codes from the local service logs.

Restore the normal order timers afterwards:

```bash
docker compose up -d order-service
```

See `e2e/README.md` for configuration, individual test commands, and the separate observability test.

## Observability

Start the optional monitoring stack:

```bash
docker compose --profile observability up --build --wait
```

Grafana is available at **http://127.0.0.1:3000** using the credentials configured in `.env`.

Alloy collects container logs into Loki and sends service health probe results to Prometheus. Grafana provides the provisioned FoC overview dashboard.

## Repository Layout

```text
.
├── api-gateway/       Public API routing and session handling
├── user-service/      Accounts and authentication
├── supplier-service/  Campus suppliers
├── order-service/     Errand lifecycle and scheduling
├── credit-service/    Credit ledger and settlement
├── chat-service/      Messaging and media
├── frontend/          React application
├── e2e/               Whole-stack tests
├── data/              Supplier seed data
├── rabbitmq/          Broker configuration
├── observability/     Logging, monitoring, and dashboards
├── scripts/           Local demonstration utilities
├── ai/                Recorded decisions and AI usage disclosures
└── compose.yaml       Local backend and infrastructure
```

## Documentation

Service setup and implementation details:

- `api-gateway/README.md`
- `user-service/README.md`
- `supplier-service/README.md`
- `order-service/README.md`
- `credit-service/README.md`
- `chat-service/README.md`
- `e2e/README.md`

Committed API specifications are in each applicable service’s `api/openapi.yaml`. The gateway is implemented code-first.

Recorded team decisions are in `ai/decisions.md` and `ai/decisions/`. Repository contribution and AI assistance rules are in `AGENTS.md`.

## AI Use Summary

AI assistance has been used for implementation, scaffolding, debugging, refactoring, testing, and documentation. Session-level disclosures, exact prompts, affected areas, and human review status are recorded in `ai/usage-log.md`; affected files also contain attribution comments.

Requirements, architecture, data schemas, API interfaces, and decision rationales remain human responsibilities under the project’s CS3219 AI usage policy. Pending review entries remain marked `PENDING` until reviewed by a human.

## Team Members

| Name | Main Service Contribution |
| ----- | ----- |
| Nigel Teo Zi You | UI + API Gateway |
| Lim Zi Yang | User Service |
| Tay Xu Xuan Seth | Credit Service |
| Goh Chee Yang | Supplier Service |
| Teo Wee How James | Order Service |

