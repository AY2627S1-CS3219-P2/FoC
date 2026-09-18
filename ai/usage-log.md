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
| 2026-09-18T11:28 | Lim-ZY | Codex (GPT-5) | generate | `user-service/Dockerfile`, root `compose.yaml`, `user-service/.dockerignore` — containerized user-service API with PostgreSQL 16 and local build exclusions | Great. Go ahead and implement those now. Then use the $present-changes-visually skill to show the changes you made. Do not commit anything. | Added a multi-stage Go Dockerfile, PostgreSQL 16/user-service Compose services with health-gated startup and persistent database volume, and a Docker build context exclusion file. Generated a self-contained visual diff page; did not commit changes. | COMPLETE |
| 2026-09-18T11:43 | Lim-ZY | Codex (GPT-5) | docs | `user-service/_temp/user-service-scaffold-plan.md` — planning-only scaffold checklist based on the D1 backlog and recorded service instructions | Now based on the following product backlog, use the [$iterative-tdd](/home/klzy/Documents/NUS/Y3S1/CS3219/Project/FoC/.agents/skills/iterative-tdd/SKILL.md) skill to scaffold the user service. You should only touch the user-service subdirectory, but you may read through the whole repository for context first. | Inspected the backlog PDF and repository guidance, distinguished recorded requirements from unresolved API/schema/security decisions, and wrote a scaffold-only checklist. No application code was changed; awaiting human plan approval. | COMPLETE |
| 2026-09-18T11:52 | Lim-ZY | Codex (GPT-5) | generate | `user-service/go.mod`, `user-service/internal/config/config.go`, `user-service/internal/config/config_test.go`, and the iteration plan/diff — initial module and environment configuration scaffold | Great the checklist looks good. Go ahead and proceed to implementing. | Added the module metadata and tested configuration loader for PORT, USER_DB_URL, and the recorded JWT setting names. gofmt passed; go test and go vet were attempted but the installed Go runtime could not recognize its own standard-library packages. Generated a visual diff; did not commit. | COMPLETE |
