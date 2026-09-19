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
| 2026-09-19T14:45 | Nigeltzy | Claude Code (Opus 5) | docs | `ai/decisions.md` — recovered from the unmerged PR #2, where it was the only copy, and appended rows D-010..D-018 plus a rewritten Open table. New `ai/decisions/D-010-api-gateway-auth.md` and the team's `jwt-token-architecture-diagram.png` | Commit whatever we currently have, then can you check through all the branches to see if anyone has implemented an API gateway similar to my diagram here? (diagram attached) -> Our team discussed and ended up with the architecture diagram i am pasting here (architecture diagram + token-lifecycle write-up attached) -> Just tell me clearly exactly what my next few steps are -> 1. Go ahead and commit 2. I have included it 3. If Redis only holds hashed values and is safe to keep in the public domain, then the gateway can manage it (just checking to make sure that it is separate from the user DB) 4. Just use whatever ports are default and work for the time being 5. Yes so far just keep it invented, unless another git branch already has the frontend auth paths written. 6. Where is the rationale cells | Searched every ref on origin including all three PR heads and every tree in history, and reported that no API gateway exists anywhere. Treated the team's diagram plus written lifecycle as a recorded decision under root §1 — a written team artifact, as distinct from a decision asserted only in a prompt — and transcribed it rather than declining. **Every Rationale cell left blank**, which is the part §1 reserves for a human. Noticed `ai/decisions.md` had never reached `main` (PR #2 was superseded by PR #3) and recovered it before adding to it. Refused to resolve the Redis conflict itself — D-013 has the gateway reading Redis while D-014 has user-service writing it, which root §4.1 and D-003 forbid — and put it to the owner, who answered that the gateway owns it (now D-017). Corrected the premise behind that answer rather than accepting it: Redis holds `jti` and `suspended:<uid>` identifiers, not hashes; the hashed refresh token lives in the User DB per D-012; and write access to Redis is enough to un-revoke a token, so it must not be publicly exposed. Recorded the consequence the answer creates — D-014 has user-service writing Redis directly, which D-017 no longer permits — as a new Open row rather than inventing the revocation API it implies. | PENDING |
| 2026-09-19T14:50 | Nigeltzy | Claude Code (Opus 5) | generate | New `api-gateway/` — `go.mod`, `cmd/api/main.go`, `internal/config` (+ empty test), empty `internal/auth|revocation|proxy|httpapi` `doc.go` files, `api/README.md`, `Dockerfile`, `.env.example`, `AGENTS.md`, `CLAUDE.md`, `README.md`. Root `AGENTS.md` §3 layout and port table; root `.env.example` gateway block | (same session as the decision-record entry above) ... 1. Go ahead and commit ... 4. Just use whatever ports are default and work for the time being | Kept strictly to scaffolding as root §1 defines it — directories, module, wiring, config plumbing, empty test file — and wrote no token verification, no Redis client and no proxying. Each empty package's `doc.go` names what blocks it rather than leaving it silent. `config.Load` returns an error listing every missing variable instead of falling back to defaults, so a missing value fails at boot rather than silently addressing the wrong thing. Added no dependency: the module requires nothing beyond the standard library, and `chi/v5` (D-002) was deliberately left out until the first real route exists. Ports 8080/6379 were taken from the owner's instruction and recorded as **provisional** (D-018) rather than presented as settled. Flagged both shared-file touches (root `AGENTS.md`, root `.env.example`) per root §3. **NOT COMPILED OR VETTED** — no Go toolchain is installed on this machine, so `go build`, `go vet` and `gofmt` have not been run against any of it. | PENDING |
| 2026-09-19T19:55 | Nigeltzy | Claude Code (Opus 5) | edit | `api-gateway/internal/revocation/doc.go`, `api-gateway/AGENTS.md`, `api-gateway/.env.example` — doc comments and config comments only, no behaviour | Asked whether the endpoints in Zi Yang's user-service AGENTS.md required changes here | Answered no: that file is on no branch, so root §1 does not count it as recorded, and its paths are user-service-facing while the frontend addresses the gateway. Did change what D-020 made factually wrong — three passages still said D-017 gave Redis to the gateway exclusively and that user-service had to ask the gateway to revoke | PENDING |
