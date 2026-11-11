<!--
AI Assistance Disclosure:
Tool: Antigravity (model: Gemini 2.5 Pro), date: 2026-11-11
Scope: Updated main README with notification-service, errand ranking/ratings, and polished system documentation.
Author review: PENDING — reviewer to complete
-->

# CS3219 — Software Design and Architecture (AY2627 Sem 1)

## Friend on Campus (FoC)

**Friend on Campus (FoC)** is a peer-to-peer campus errand platform where NUS students can request items to be collected from stores or facilities on campus, and other students can fulfil (and deliver) those requests. The platform runs on a closed credit economy — credits cannot be bought, withdrawn, or exchanged for money, and only circulate within the platform.

---

## Features

- **Accounts & Authentication:** NUS email registration, OTP verification (local log or SendGrid), RS256 JWT sessions, Redis-backed token revocation, logout, and profile management.
- **Campus Suppliers:** Browse and search verified campus food courts, canteens, and stores, view their operating details and menus, and manage them through admin controls.
- **Errand Lifecycle:** Post immediately or schedule for later, amend details and rewards, accept, withdraw, collect, deliver, and confirm completion with robust distributed state safeguards.
- **Intelligent Errand Ranking:** Personalized courier recommendations scored using tree-based machine learning models based on historical habits, campus transit times, route overlap, and ratings.
- **Mutual Ratings & Feedback:** 5-star ratings and feedback between requesters and couriers upon errand completion to build campus community trust.
- **Lifecycle Automation:** Automated background workers expire unaccepted errands and automatically confirm deliveries after configurable grace periods.
- **Closed Credit Economy:** Initial grant allocations, double-entry immutable ledger, escrow reward reservations, courier payouts, cancellation refunds, and periodic ledger reconciliation.
- **Real-Time Chat:** Errand-scoped 1:1 requester–courier messaging with WebSocket real-time updates, image attachments, read receipts, and lifecycle-based room locking.
- **In-App Notifications:** Event-driven in-app notifications and toast alerts for errand lifecycle milestones with resilient drop-head queuing and read status tracking.
- **Administration:** Account roles and permission controls, supplier curation, errand oversight, flagged credit account integrity review, and immutable audit logs.
- **Observability:** Centralised container logs via Loki, Prometheus health probing and metrics via Grafana Alloy, and pre-configured Grafana dashboards.

---

## System Overview

The React frontend communicates exclusively through a single public **API Gateway**. The gateway validates authentication tokens, checks revocation status against Redis, handles rate limiting, and forwards API traffic to six private internal Go microservices:

| Component | Port | Database / State | Responsibility |
| --- | --- | --- | --- |
| [`api-gateway`](api-gateway/README.md) | `8080` (public) | Shared Redis (revocation) | Public API ingress, JWT RS256 verification, token revocation checks, reverse-proxy routing, and refresh-cookie handling |
| [`user-service`](user-service/README.md) | `8081` (internal) | `user-db`, `user-ranking-db` | Accounts, authentication, OTPs, student profiles, roles, and ML ranking model storage |
| [`supplier-service`](supplier-service/README.md) | `8082` (internal) | `supplier-db` | Campus suppliers, pickup locations, merchant directory, and supplier administration |
| [`order-service`](order-service/README.md) | `8083` (internal) | `order-db` | Errand lifecycle, scheduling, state machine, courier ranking engine, ratings, and outbox event publishing |
| [`credit-service`](credit-service/README.md) | `8084` (internal) | `credit-db` | Double-entry ledger, escrow reward reservations, settlement, audit reconciliation, and admin dispute reviews |
| [`chat-service`](chat-service/README.md) | `8085` (internal) | `chat-db`, `chat-redis`, media volume | Errand-scoped 1:1 conversations, WebSocket messaging hub, image uploads, and room lifecycle sync |
| [`notification-service`](notification-service/README.md) | `8086` (internal) | `notification-db` | In-app errand notifications, unread tracking, and event-driven alerts from RabbitMQ lifecycle events |

### Architecture Highlights

- **Database-Per-Service:** Every microservice owns its isolated PostgreSQL 16 database container. No service queries another service's database directly.
- **Strict Ingress Boundary:** The API Gateway (`:8080`) is the only application service published to the host in Docker Compose. All domain services run in a private internal container network.
- **Event-Driven Decoupling:** Asynchronous cross-service workflows (credit settlements, chat room lifecycle, in-app notifications) are propagated through RabbitMQ using the `foc.events` topic exchange and dedicated dead-letter exchanges (`foc.dlx`).
- **Synchronous Consistency:** Synchronous REST calls are strictly limited to critical boundary validations (e.g., `order-service` calling `credit-service` for atomic escrow reward holds and `supplier-service` for merchant verification).

---

## Technology Stack

| Area | Technologies |
| --- | --- |
| **Frontend** | React 19, TypeScript, Vite, Tailwind CSS, Lucide Icons |
| **Backend** | Go 1.26, Chi router, `oapi-codegen` |
| **Persistence** | PostgreSQL 16, `pgx/v5`, `golang-migrate` |
| **Messaging & Events** | RabbitMQ (topic exchange `foc.events`, DLX `foc.dlx`, drop-head queues) |
| **Caching & In-Memory State** | Redis (token revocation, temporary state), Chat Redis (presence & pub/sub) |
| **Machine Learning / Ranking** | Decision forest / tree-based ranking models (`order-service` & `user-ranking-db`) |
| **Local Runtime** | Docker Compose |
| **Observability** | Grafana, Grafana Loki, Grafana Alloy, Prometheus |
| **Testing** | Go `testing`, `testify`, `testcontainers-go` (Docker), Vitest, React Testing Library |

---

## Getting Started

### Prerequisites

- **Docker** with **Docker Compose** (Docker Desktop, Colima, or Docker Engine)
- **Node.js** (v20+ recommended) and **npm**
- **Python 3** with `cryptography` (for generating local JWKS signing keys)
- **Go 1.26** or later (optional, for running backend tests or services outside Docker)

---

### 1. Configure the Backend

From the repository root:

```bash
cp .env.example .env
```

Ensure the initial admin credentials in `.env` match your preferences. The default demo account configuration:

```dotenv
INITIAL_ADMIN_EMAIL=admin@u.nus.edu
INITIAL_ADMIN_USERNAME=admindemo
INITIAL_ADMIN_PASSWORD=AdminDemo123
OTP_DELIVERY=log
```

> [!NOTE]
> The admin account is automatically seeded on first startup when `user-service` detects no existing admin account in the database. Keep `.env` and private signing keys out of version control.

---

### 2. Generate Local Signing Keys

The backend uses RS256 JWTs. Generate the JSON Web Key Set (JWKS) before running the containers:

```bash
python3 -m venv .venv
.venv/bin/python -m pip install cryptography
.venv/bin/python user-service/generate_jwks.py --out user-service/keys.json
```

Docker Compose mounts `user-service/keys.json` directly into `user-service`. It is not included in the container image.

---

### 3. Start the Backend

From the repository root:

```bash
docker compose up --build --wait
```

The gateway is available at `http://localhost:8080`. Its health endpoint is:

```bash
curl http://localhost:8080/healthz
```

---

### 4. Start the Frontend

In a separate terminal:

```bash
cd frontend
cp .env.example .env
npm ci
npm run dev
```

Open your browser at **http://localhost:3001**.

> [!TIP]
> Leave `VITE_GATEWAY_BASE_URL` empty and `VITE_USE_FIXTURES` unset for the full application. Vite forwards `/auth` and `/api` to the gateway, including chat WebSocket traffic.

---

### 5. Try the Application

1. **Sign in as Admin:**
   - Sign in at `http://localhost:3001` with username `admindemo` and password `AdminDemo123` to access administration.
   - Inspect user roles, manage campus suppliers, view audit logs, and inspect flagged credit accounts.

2. **Register a Student Account:**
   - Register through the frontend (e.g. `student1@u.nus.edu`).
   - With `OTP_DELIVERY=log`, retrieve the one-time registration code from the logs:
     ```bash
     docker compose logs user-service
     ```
   - Input the code in the frontend to complete verification.
   - (`OTP_DELIVERY=log` is intended for local development. SendGrid delivery is also supported; see [`user-service/README.md`](user-service/README.md)).

3. **Complete an Errand Flow:**
   - Register a second student account (`student2@u.nus.edu`).
   - Use two student accounts to try the complete requester–courier workflow:
     1. Post an errand as Requester with a reward.
     2. Browse available errands as Courier (recommended via the personalized ranking engine) and accept it.
     3. Chat in real time via the errand chat room and send messages or images.
     4. Mark pickup and delivery as Courier.
     5. Confirm completion as Requester.
     6. Inspect updated credit balances and rate each other.

---

### Stop the Backend

```bash
docker compose down
```

To stop the optional observability services too:

```bash
docker compose --profile observability down
```

> [!CAUTION]
> Named volumes retain local data. Adding `-v` deletes those volumes, including databases and uploaded chat media:
> ```bash
> docker compose down -v
> ```

---

## Testing

### Backend Unit Tests & Checks

Run inside the backend service being changed:

```bash
go test ./...
go vet ./...
```

Each service is a separate Go module; repeat these commands for the relevant services.

### Backend Integration Tests

For services with integration tests, run inside that service directory with Docker available:

```bash
go test -tags=integration ./...
```

### Frontend Tests

From `frontend/`:

```bash
npm test
npm run build
```

### Whole-Stack Tests

From the repository root, after completing local configuration:

```bash
ORDER_EXPIRY_PERIOD=20s ORDER_CONFIRMATION_WINDOW=20s \
  docker compose up --build --wait

go -C e2e test -tags=e2e -count=1 ./...
```

The suite exercises the gateway against real services, including account verification, errand settlement, scheduling, concurrent acceptance, refunds, token revocation, supplier administration, chat, audit logs, notifications, and ranking.

Keep `OTP_DELIVERY=log` for this suite. It reads verification codes from the local service logs.

Restore the normal order timers afterwards:

```bash
docker compose up -d order-service
```

See [`e2e/README.md`](e2e/README.md) for configuration, individual test commands, and the separate observability test.

---

## Observability

Start the optional monitoring stack:

```bash
docker compose --profile observability up --build --wait
```

- **Grafana:** Available at **http://127.0.0.1:3000** using the credentials configured in `.env`.
- **Alloy & Loki:** Alloy collects container logs into Loki.
- **Prometheus:** Scrapes service health probe results and container metrics.
- Grafana provides the pre-provisioned FoC overview dashboard.

---

## Repository Layout

```text
.
├── api-gateway/          Public API routing and session handling
├── user-service/         Accounts, authentication, and ranking models
├── supplier-service/     Campus suppliers and store directory
├── order-service/        Errand lifecycle, scheduling, ranking, and ratings
├── credit-service/       Credit ledger, escrow reservations, and settlement
├── chat-service/         Errand-scoped messaging, media, and WebSocket hub
├── notification-service/ In-app errand notifications from lifecycle events
├── frontend/             React 19 application
├── e2e/                  Whole-stack end-to-end test suite
├── data/                 Supplier seed data
├── rabbitmq/             Broker configuration and definitions
├── observability/        Logging, monitoring, and Grafana dashboards
├── scripts/              Local demonstration utilities
├── ai/                   Recorded decisions and AI usage disclosures
├── compose.yaml          Local backend and infrastructure
└── AGENTS.md             Repository contribution, agent guidance, and domain conventions
```

---

## Documentation

Service setup and implementation details:

- [`api-gateway/README.md`](api-gateway/README.md)
- [`user-service/README.md`](user-service/README.md)
- [`supplier-service/README.md`](supplier-service/README.md)
- [`order-service/README.md`](order-service/README.md)
- [`credit-service/README.md`](credit-service/README.md)
- [`chat-service/README.md`](chat-service/README.md)
- [`notification-service/README.md`](notification-service/README.md)
- [`e2e/README.md`](e2e/README.md)

Committed API specifications are in each applicable service’s `api/openapi.yaml`. The gateway is implemented code-first.

Recorded team decisions are in [ai/decisions.md](ai/decisions.md) and [ai/decisions/](ai/decisions/). Repository contribution and AI assistance rules are in [AGENTS.md](AGENTS.md).

---

## AI Use Summary

AI assistance has been used for implementation, scaffolding, debugging, refactoring, testing, and documentation. Session-level disclosures, exact prompts, affected areas, and human review status are recorded in [ai/usage-log.md](ai/usage-log.md); affected files also contain attribution comments.

Requirements, architecture, data schemas, API interfaces, and decision rationales remain human responsibilities under the project’s CS3219 AI usage policy. Pending review entries remain marked `PENDING` until reviewed by a human.

---

## Team Members

| Name | Main Service Contribution |
| ----- | ----- |
| Nigel Teo Zi You | UI + API Gateway + Notification Service |
| Lim Zi Yang | User Service |
| Tay Xu Xuan Seth | Credit Service |
| Goh Chee Yang | Supplier Service |
| Teo Wee How James | Order Service |
