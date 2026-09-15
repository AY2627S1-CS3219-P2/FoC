# Decision Record

The team's decisions, written down so they can be built against. Root
`AGENTS.md` §1 treats a decision as **made** only if it appears here, in a
merged `api/openapi.yaml`, or in code already on `main`. Anything else is still
open, and an AI tool must hand it back rather than pick an answer.

**This file is human-written.** Requirements, architecture, service boundaries,
design patterns, data schemas, API interfaces and the *reasoning* behind them
are exactly what CS3219 Appendix 2 forbids delegating. An AI tool may transcribe
a decision you have already made, and may implement one, but must not fill in a
`Rationale` cell or add a row for a question you have not answered.

Rationale matters beyond compliance: D3 and D4 grade "technical depth →
rationale for decisions is explained; trade-offs and integration considerations
addressed." These rows are the raw material for those slides.

## How to add one

Append to the table. Keep the statement short enough that someone can build
against it without asking a follow-up question. If a decision needs more than a
few lines, write it up in `ai/decisions/<id>-<slug>.md` and link it.

| ID | Date | Decided by | Decision | Rationale | Status |
| --- | --- | --- | --- | --- | --- |
| D-001 | 2026-09-15 | team | Backend services are written in Go | | Accepted |
| D-002 | 2026-09-15 | team | HTTP routing uses `chi/v5`; persistence uses `pgx/v5` against PostgreSQL; migrations use `golang-migrate` | | Accepted |
| D-003 | 2026-09-15 | team | Each service owns its own PostgreSQL database exclusively; no shared schema and no cross-service table access | | Accepted |
| D-004 | 2026-09-15 | team | Each service is a separate Go module (`foc/<service>`), not one root module | | Accepted |
| D-005 | 2026-09-15 | team | API contracts are spec-first: `<service>/api/openapi.yaml` is authored first, Go types and the server interface are generated from it into `internal/gen/`, and generated code is committed | | Accepted |
| D-006 | 2026-09-15 | team | CI runs on GitHub Actions | | Accepted |
| D-007 | 2026-09-15 | team | Local deployment is Docker Compose from a single root `compose.yaml` | | Accepted |
| D-008 | 2026-09-15 | team | Local port allocation: user 8081, supplier 8082, order 8083, credit 8084, frontend 3001 | | Accepted |
| D-009 | 2026-09-15 | team | Cloud provider is AWS. The compute target and IaC tool are **not** decided yet and nothing is to be scaffolded for them | | Partial |

## Open — do not implement until these have a row above

Named here so an agent can point at the gap instead of guessing. Each is
graded at D2 or D3, so the answer needs a rationale written alongside it.

| Question | Blocks | Milestone |
| --- | --- | --- |
| Frontend language and framework | `frontend/` beyond the D1 prototype | D2 |
| How identity crosses service boundaries (the `X-User-Role` header in `supplier-service` is an interim stand-in) | User Service, and admin auth in every service | D2 |
| Whether requester/courier/admin are roles, a toggle, or modes of one account | User Service data model | D2 |
| The order state set and which transitions are legal | Order Service | D3 |
| How concurrent acceptance of the same errand is made safe | Order Service | D3 |
| Whether credit reservation is atomic with order persistence | Order + Credit Services | D3 |
| What happens to an errand that stalls after acceptance | Order Service | D3 |
| How credit-operation atomicity is achieved | Credit Service | D3 |
| Which interactions are asynchronous, and the broker technology | Async workflow (M6) | D3 |
| The event schema, and how duplicate events are handled | Order + Credit Services | D3 |
| AWS compute target (ECS Fargate / EKS / other) and IaC tool | Cloud deployment | D3–D4 |
