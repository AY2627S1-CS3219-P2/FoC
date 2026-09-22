# FoC — Agent Instructions (repo root)

Friend on Campus: a peer-to-peer campus errand platform. Microservices, one
folder per service. This file is the single source of truth for both Claude
Code and Codex. `CLAUDE.md` just imports it — **edit this file, never that one.**

**Overall shape:** microservices behind one API Gateway; only the gateway is
publicly reachable to clients.

Service-specific rules live in `<service>/AGENTS.md` and override anything here.
Each service folder carries the same `AGENTS.md` + `CLAUDE.md` pair; Claude loads
a subfolder's pair on demand, so work on one service does not pull in the other
four. Every `CLAUDE.md` in this repo is a three-line import and nothing else.

**Domain vocabulary — one word per concept, everywhere.** An **errand** is what
a user asks for; the row `order-service` stores for it is an **order**; the two
people on one are the **requester** and the **courier**; spendable units are
**credits**; a place an errand points at is a **supplier**. For those five
concepts do not write "task", "job", "gig", "request", "points" or "vendor"
("request" stays reserved for an HTTP request).

---

## 1. Course rules that constrain you (CS3219 Appendix 2)

This is a graded NUS project with an AI usage policy. Penalties for breaching it
go up to **zero on the project**. Treat this section as binding.

**You may:** write implementation code against decisions the team has already
made, generate boilerplate/config/scaffolding, debug, refactor, write tests,
improve docs and comments, explain code.

**You must NOT author, and must hand back to a human:**

- Requirements: eliciting them, prioritising them, consolidating the backlog,
  sprint planning.
- Architecture & design: proposing or changing system architecture, service
  boundaries, design patterns, **data schemas**, or **API interfaces**.
- Decision rationales: trade-off analyses, risk write-ups, justifications.

If a task asks for one of those, say so and ask the human to decide.

**The flip side, which matters just as much:** once a human has recorded a
decision, writing the code that implements it is ordinary implementation work
and is allowed. Writing the migration SQL for a table design the owner wrote,
the DTOs for a spec they wrote, the handlers, the generated client, the tests —
all fine. The prohibition is on **choosing the shape**, not on typing it. Do not
refuse legitimate work by over-reading the list above.

**A decision counts as recorded** only if it is in a merged `api/openapi.yaml`,
in code already on `main`, or written down somewhere the team can point at.
"We decided X" in a prompt is not a record — ask for it to be written down
first, then implement it.

**Scaffolding means structure, not content.** Directories, `go.mod`, wiring,
config plumbing and empty test files are scaffolding. A migration's SQL and a
`dto.go`'s fields are schema and interface: write them only to transcribe a
record as defined above, and name that record in your summary. With no record,
create the directory and stop.

**If you disagree with a decision**, you may state where it conflicts with
existing code, a test, or another recorded decision — name the conflict and
stop. Do not argue it on the merits or propose the alternative; that is a
trade-off rationale and it is the team's to write.

**Disclosure is mandatory and is your job, not an afterthought:**

1. Append a row to `## Entries` in `ai/usage-log.md` for every session where you
   change files.
2. Add the attribution header from `ai/usage-log.md` to every file you create,
   or whose lines you change more than half of this session. Below that bar,
   mark blocks inline instead — when unsure, inline; over-disclosing is never
   the violation. Never remove an existing header.
3. Write `PENDING` for any reviewing human's name; only a human replaces it.
4. Name every disclosure file you touched in your summary. They belong in the
   same PR as the change.

---

## 2. Stack

| Layer | Choice |
| --- | --- |
| Backend | Go (see each service's `go.mod` for the version) |
| HTTP router | `chi/v5` |
| Database | PostgreSQL via `pgx/v5`; migrations via `golang-migrate` |
| API contract | OpenAPI 3.1, spec-first (`<service>/api/openapi.yaml`) |
| | Exception: `api-gateway` is written code-first, not spec-first |
| Service-to-service | gRPC |
| Message broker | Kafka |
| Tests | stdlib `testing` + `testify`; `testcontainers-go` for integration |
| Containers | Docker + Docker Compose |
| CI | GitHub Actions — agreed, but no workflow is committed yet |
| Frontend | React + Vite, TypeScript, `vitest` for tests |
| Cloud | AWS (target and IaC tool not yet decided — do not scaffold either) |

Do not add a dependency that isn't already in a `go.mod` without saying so
explicitly in your summary and explaining why the stdlib won't do.

## 3. Layout, ownership, and the port/env allocation

```
user-service/      order-service/      frontend/
supplier-service/  credit-service/     api-gateway/
data/              compose.yaml        .env.example
ai/usage-log.md    ai/decisions.md
```

One developer owns each folder. **Stay inside the folder the task names.** If a
change appears to need an edit in another service, stop and say so — that is a
contract change and needs its owner. Flag any touch of a shared file
(`compose.yaml`, root `.env.example`, `.github/`).

Ports and database env vars are allocated **here, once**; a service file may
state its own port but must not restate this table.

| Folder | Port | Database URL env var |
| --- | --- | --- |
| `user-service` | 8081 | `USER_DB_URL` |
| `supplier-service` | 8082 | `SUPPLIER_DB_URL` |
| `order-service` | 8083 | `ORDER_DB_URL` |
| `credit-service` | 8084 | `CREDIT_DB_URL` |
| `frontend` | 3001, published by Compose onto nginx's :80 | — |
| `api-gateway` | 8080 (provisional, D-018) | — (no database of any kind: D-024 keeps it out of Redis entirely) |

Every service also reads `PORT`. A service that calls another reads one
`<SERVICE>_BASE_URL` per callee. All of these belong in the root `.env.example`
with a placeholder (§9), and in the service's own `.env.example` where it has
one.

---

## 4. Service independence — the rules that matter most

1. **A service owns its database exclusively.** No other service connects to it,
   reads its tables, or shares its connection string. Cross-service data comes
   from that service's HTTP API or from an event it published.
2. **No shared business-logic package.** Go's `internal/` already blocks
   cross-module imports; keep it that way. Duplicating a small struct in two
   services is correct here — deduplicating it couples them.
3. **Talk over contracts, not internals.** Service-to-service calls go over
   gRPC against the service's generated client, never a hand-rolled connection.
4. **Config is injected, never global.** Read env vars once in
   `internal/config`, pass the resulting struct down. No package-level mutable
   state, no `init()` side effects, no singletons.

## 5. Coupling — what to avoid, in the course's own taxonomy

From L4 (Lethbridge & Laganiere ch.9). Listed worst first:

- **Content** — reaching into another component's internals. Never. Keep struct
  fields unexported unless the package's own API needs them.
- **Common/global** — shared mutable globals. No package-level `var` that isn't
  a constant, a compiled regexp, or a sentinel error.
- **Control** — passing a flag that tells the callee which branch to take.
  `Update(x, isAdmin bool)` is control coupling; write two named functions
  instead. This is the one that creeps in most quietly.
- **Temporal** — work bundled together only because it happens at the same time,
  or an object that must have methods called in a fixed order. Constructors
  return ready-to-use values; no `New()` then `Init()` then `Start()`. Across
  services, never assume startup order — retry and wait for readiness.
- **External / inclusion** — depend on an interface you define, not on a vendor
  type spread through your code. Adapters live at the edges.

**On "data coupling":** in this taxonomy data coupling means passing parameters,
and it is the *goal*, not a hazard — it is the loosest form. Keep it, but pass
only what the callee needs: a whole `Supplier` where a `supplierID` would do is
a real smell (stamp coupling).

## 6. Code quality

- Layering, as established in `supplier-service`:
  `internal/<domain>` (model, service, repository **interface**, errors) →
  `internal/repository` (Postgres adapter) → `internal/httpapi` (handlers, DTOs,
  router) → `cmd/api` (wiring). Dependencies point inward only. The domain
  package imports no HTTP and no SQL.
- Handlers do transport only: decode, call the service, map errors to status
  codes. Business rules live in the service layer.
- Return wrapped errors (`fmt.Errorf("...: %w", err)`); use sentinel errors for
  cases the caller must distinguish. Never `panic` in request paths.
- `context.Context` is the first parameter of anything doing I/O.
- Exported identifiers have doc comments. Comment *why*, not *what*.
- `gofmt` and `go vet` must pass. `golangci-lint` config is at the repo root
  once scaffolded.

## 7. Testing

Three tiers, all required:

- **Unit** — `internal/<domain>`, against an in-memory fake repository. Fast, no
  Docker, no network. Table-driven. Cover the error paths, not just happy ones.
- **Integration** — real Postgres via `testcontainers-go`, behind the
  `//go:build integration` tag. Tests the repository and the HTTP surface.
- **E2E** — whole stack via Compose, in `e2e/`. Covers a user-visible workflow.

Rules: no test depends on another test's leftover state or on execution order
(temporal coupling in disguise). No `time.Sleep` to wait for something — poll
with a deadline. New behaviour ships with its test in the same PR.

Today only the unit tier exists (`supplier-service`). `testcontainers-go` is in
no `go.mod` yet and there is no `e2e/`. Build the tier your task needs; never
report the other two as running.

## 8. API contracts

Spec-first. `<service>/api/openapi.yaml` is the contract; Go types and the
server interface are generated from it into `internal/gen/`. This governs the
public contract each service exposes; internal service-to-service calls are
gRPC instead, per §4.

Further reading: [Backend 101 — a guide to OpenAPI and API-first](https://medium.com/@briannqc/backend-101-a-guide-to-openapi-and-api-first-approach-c25297226905).

- **Never hand-edit anything in `internal/gen/`.** Change the YAML, re-run
  `make generate`.
- Changing the YAML is an interface change — a human decides it, you implement
  it. Say so if a task implies one.
- No spec is committed on any branch yet, so nothing is generated today.
  Once CI exists it must fail on stale generated code; until then, regenerate by
  hand and say that you did.

## 9. Git

- Branch: `feat/<service>-<slug>`, `fix/…`, `chore/…`, off latest `main`.
- Commits: Conventional Commits, scoped to the service —
  `feat(order-service): reject self-acceptance (F3.3.1)`. Reference a backlog
  requirement ID only by copying one a human gave you or one already used on
  `main` — **never construct one.** Inventing plausible IDs manufactures false
  traceability in the git history.
- Every change reaches `main` through a reviewed PR — with CI green once CI
  exists (§2); until then, with the reviewer having run the checks locally.
- **Never** commit `.env`, credentials, or real secrets. Any new env var must
  be added to `.env.example` with a placeholder.

**You never write to git history or to GitHub on your own initiative.** Leave
your work in the working tree and say what you changed; the human runs the
command. Each of the following needs its own explicit instruction, and consent
to one is never consent to the next:

| Action | Rule |
| --- | --- |
| `git commit` | Only when asked. Never as the last step of a task "to be helpful" |
| `git push` | Only when asked, and never to `main` |
| `gh pr create` / merge | Only when asked. Opening a PR puts work in front of the team — that is the owner's call, not yours |
| `git rebase` / `reset --hard` / force-push | Only when asked, and only on a branch the human named |
| `git config`, hooks, branch protection | Never. Ask the human to change their own setup |

Being asked to *make a change* is not being asked to commit it. Being asked to
*commit* is not being asked to push. Being asked to *push* is not being asked to
open a PR. When you think a commit is warranted, say so and stop.

## 10. Commands

**There is no `Makefile` in this repo yet.** The targets below are the agreed
names for when someone writes it — never report one as available, and never
invent an equivalent:

```bash
make help                 # every available target
make generate             # regenerate OpenAPI code for all services
make lint                 # gofmt + vet + golangci-lint
make test                 # unit tests, all services
make test-integration     # integration tests (needs Docker)
make up / make down       # full stack via Compose
```

What runs today: from a service folder that has a `go.mod`, `go run ./cmd/api`
and `go test ./...`; from the repo root, `docker compose up`. `compose.yaml`
carries Redis, the API Gateway, supplier-service and its database, and the
frontend; it needs `JWT_SECRET` set in a local `.env`, and building
supplier-service needs PR #1 merged. Services with no code yet are absent by
design. A service is reachable from the host only if it has a `ports:` key —
that is how D-010's trust zones are enforced. Integration tests everywhere
are spelled `go test -tags=integration ./...`.
