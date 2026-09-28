# AI Usage Log

Required by CS3219 Appendix 2: _"Maintain a log, `/ai/usage-log.md`, in the
repository with timestamps, prompts, and usage scenarios."_ This log is shared —
every team member appends to it, and so does every AI tool that changes files.

The consolidated AI Use Summary required as the README's last section is
generated from this log, so keep entries accurate rather than tidy. What may and
may not be delegated to an AI tool is defined in **§1 of the root `AGENTS.md`**
— that is the single copy; do not restate it here.

## How to add an entry

Append one row per working session to the table under [Entries](#entries),
newest at the bottom. That is the only table in this file.

| Column        | What goes in it                                                                                                      |
| ------------- | -------------------------------------------------------------------------------------------------------------------- |
| Date/Time     | ISO 8601 local, e.g. `2026-09-15T18:40`                                                                              |
| Member        | The human accountable. An agent fills this from `git config user.name`                                               |
| Tool / model  | e.g. `Claude Code (Opus 5)`, `Codex (GPT-5)`                                                                         |
| Mode          | `generate`, `refactor`, `debug`, `explain`, `docs`, or `test`                                                        |
| Scope         | Files or areas touched, and what changed                                                                             |
| Prompt        | The prompt as actually typed                                                                                         |
| Key responses | What the tool produced or concluded in reply — the decision it made, the approach it took, or what it declined to do |
| Human review  | What you checked and verified — `PENDING` until a human has                                                          |

**Prompts.** Record the prompt as actually typed, not a cleaned-up version — the
policy asks for "the exact prompts", and a reconstruction that reads better than
the original is itself a disclosure problem.

## File-header attribution

Every file an AI tool created, or whose lines it changed more than half of in
one session, carries this block at the top in that file's comment syntax:

```
// AI Assistance Disclosure:
// Tool: <product> (model: <model>), date: <YYYY-MM-DD>
// Scope: <what it generated or changed in this file>
// Author review: PENDING — <reviewer to complete>
```

Below that bar, mark the individual blocks inline instead:
`// AI-generated (edited by <name>).` When unsure which applies, mark inline —
over-disclosing is never the violation.

A human replaces `PENDING` with their name and what they actually checked.
Appendix 2 makes each member "fully responsible for understanding and validating
any AI-assisted code", and every member "remains accountable for understanding
the entire codebase". **Do not sign off on a file you have not read.** An agent
never writes a name into that field, and never removes an existing header.

## Entries

<!-- prettier-ignore -->
| Date/Time | Member | Tool / model | Mode | Scope | Prompt | Key responses | Human review |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 2026-09-15T18:40 | twhjames | Claude Code (Opus 5) | generate | Root + per-service `AGENTS.md`/`CLAUDE.md` and this log — agent-instruction scaffolding, no application code | we will be using claude and codex, so we need both claude.md and agents.md - do we need to have these 2 in each service folder and root folder? what content goes to which? context for our product: Go for backend services, frontend language and framework TBC, OpenAPI for API contracts and SwaggerUI for API testing and communicating specs, Docker for containerisation, GitHub Actions for CI/CD, AWS for cloud deployment plus IaC (suggest one apt for us), and 5 developers with one owning each service plus a frontend developer. we also want proper unit, integration and end-to-end testing, no code smells, and to avoid control, global and temporal coupling and other anti-patterns. | Confirmed from the Claude Code docs that Claude Code reads only `CLAUDE.md` while Codex reads `AGENTS.md`, so both filenames are needed at the root and in each service folder. Proposed a single source of truth: `AGENTS.md` carries all content and every `CLAUDE.md` is a three-line `@AGENTS.md` import, so the two tools cannot drift. Root holds what is repo-wide — stack, folder ownership, the port and env-var allocation, service-independence and coupling rules, code quality, the three test tiers, API-contract and git conventions, and the Appendix 2 boundary. Each service file holds only its own bounded context, boundaries, package layout, local development and gotchas, and references the root rather than repeating it. Root files load at session start while a service pair loads on demand, so work on one service does not pull in the other four. Separately recommended spec-first OpenAPI with `oapi-codegen` and generated code committed, Terraform as the IaC tool once AWS work starts, and splitting container work by ownership — each service's Dockerfile in that service's own PR, the shared Compose baseline in its own. | Read all six AGENTS.md; confirmed the @AGENTS.md import works in Claude Code and that Codex reads AGENTS.md. Checked usage-log.md carries the timestamp, prompt, key-response and scenario fields Appendix 2 asks for. Verified no application code written as well. |
| 2026-09-21T21:54 | GCheeYang | Claude Code (Sonnet 5) | docs | Root `AGENTS.md` only, no code — applied the same four human-authored edits from the team's `agents_md_changes.md` to the supplier PR branch: the "Overall shape" line; stack table (gRPC, Kafka, `api-gateway` code-first exception, Frontend row corrected from TBC); §4 rule 3 (OpenAPI client to gRPC); §8 scope note and reference link. Merged `origin/main` into the branch first so the file exists here | can you add the changes to the PR i created — then, clarifying which PR: the feat(supplier service) PR | Merged main (clean, no conflicts), then applied the four edits verbatim with exact-match checks; the result is identical to the same edits on `feat/api-gateway-auth-proxy`. Made no decision of its own. Does not cover this PR's earlier supplier-service and frontend work, which still needs its own log row. | PENDING |
| 2026-09-22T15:50 | GCheeYang | Claude Code (Sonnet 5) | generate | `supplier-service/` — removed `internal/httpapi` (REST/chi/cors) and replaced it with a gRPC server: new `proto/supplier/v1/supplier.proto`, generated `internal/gen/supplier/v1/`, new `internal/grpcapi/` (server, domain<->proto conversion, error-code mapping, router/health/reflection wiring), `internal/middleware/auth.go` rewritten for gRPC metadata instead of an HTTP header, `cmd/api/main.go` rewired to serve gRPC. Added tests for the new interceptor and the gRPC server (bufconn, in-process). Updated `supplier-service/README.md` and `AGENTS.md`. No change to `internal/supplier` (domain/business rules) or `internal/repository` — same schema, same validation, same soft-delete. `ai/decisions.md` does not exist on this branch (only on the unmerged api-gateway branch), so the decision is recorded in supplier-service's own AGENTS.md/README instead. | we want to change all REST to gPRC, so can we edit that and make sure that any other service that requires information from supplier service they are able to obtain, lets work on that, what you did with the FRs for the order service is great. Make sure that the other servces are able to obtain info through gPRC | Before implementing, asked the user to confirm scope (gRPC alongside REST, vs. full replacement) because full replacement breaks the gateway's REST proxy (D-027) and the prototype frontend's direct REST calls, both outside supplier-service/. User chose full replacement. Transcoded the already-existing, already-merged REST capability set (list/get/create/update/delete, same fields, same validation, same error semantics now as grpc/codes) rather than designing new capability — Order Service's needs, identified from the backlog in the prior turn, are fully covered by the existing GetSupplier/ListSuppliers RPCs. Verified with `go test ./...` and, separately, a full Docker build plus `grpcurl` against the running container (seeding, list, category filter, name search, get, admin gate with and without the metadata role, duplicate rejection, validation error, update, soft-delete, restart idempotency) — all passed. Left the REST removal's downstream breakage (gateway, frontend) explicitly documented in supplier-service/AGENTS.md and README.md as out of scope for this folder, per root AGENTS.md §3, rather than editing api-gateway or frontend. 2026-09-28 follow-up on the same branch: after the owner confirmed the `.proto` (D-035) and chose to keep the gateway/frontend's current JSON shape (D-036), pinned snake_case JSON names in the `.proto` with `json_name`, regenerated, added `internal/grpcapi/json_test.go` to pin the shape, and corrected a wrong comment in the proto that claimed the server defaults `is_available` to true (it does not). Then, on the owner's confirmation of the public URLs (D-037): added `google.api.http` annotations to all five RPCs (with `response_body: "suppliers"` so the list is a bare array, and `json_name = "q"` on `search`), regenerated, added `google.golang.org/genproto/googleapis/api` to go.mod because the generated code imports it, added `internal/grpcapi/http_annotations_test.go` to pin the routes, and updated the README and AGENTS.md. The googleapis `annotations.proto` and `http.proto` were copied from the local Python `googleapis-common-protos` package into a scratch include directory; nothing was downloaded and no third-party proto was committed. Then, on the owner's choice of identity position A (D-038): `internal/middleware/auth.go` now treats a missing or repeated `x-user-role` value as no role (tests added), comments rewritten from "interim, undecided" to the D-038 model and its two unmet conditions, and README and AGENTS.md updated. Checked PR #5 to confirm the role originates in user-service. Did not touch `compose.yaml` or `api-gateway/`: removing the published `:8082` port is a shared-file change tied to the gateway owner's change. Then, on the owner's choice of `buf` (D-039), after an intermediate choice of `protoc` that was reversed and its docs removed: installed the `buf` CLI with Homebrew, added `supplier-service/buf.yaml`, `buf.gen.yaml` and `buf.lock` (the lock pins `buf.build/googleapis/googleapis`, fetched from buf.build with `buf dep update`), regenerated `internal/gen/` with `buf generate` and diffed it against the `protoc` output (identical except the `protoc (unknown)` version header line), and rewrote the README and AGENTS.md generation sections. `buf lint` with default rules reported 8 findings, which were reported to the owner without being changed. On the owner's instruction ("lets just fix them", D-040): moved the proto to `proto/foc/supplier/v1/` so its directory matches its package (keeping the package, so gRPC method names do not change), gave every RPC its own response message with `response_body: "supplier"` on the three that return a supplier, regenerated, updated `internal/grpcapi` and its tests, confirmed `buf lint` is clean, and re-ran every RPC against the real container with grpcurl. Nothing committed; left in the working tree on branch feat/supplier-service-grpc for review. | PENDING |
