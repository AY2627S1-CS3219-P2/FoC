# 2026-09-15 — Agent instruction scaffolding

Tool: Claude Code (Opus 5). Mode: generate.

## Prompt (verbatim)

> this is just the requirements and project context. i will give you my product
> backlog and understand what we intend to build later. but for now, we want to
> scaffold the project code.
>
> a few more additional notes, we want to ensure we have proper unit testing,
> integration testing, end to end testing, no code smells and avoid control
> coupling, data coupling, global coupling, temporal coupling, anti-patterns etc.
>
> we have 5 different developers, each service is one developer and there'll
> eventually be a frontend folder taken by a frontend developer as well.
>
> for backend we are using Go language, and frontend language and framework TBC.
> cloud will be AWS, with docker containers, github actions for cicd. and we want
> to use IaC as well (suggest one apt for us). we also want to use openapi and
> swagger for backend endpoints testing and communication of API specs.
>
> we will be using claude and codex, so we need both claude.md and agents.md - do
> we need to have these 2 in each service folder and root folder? what content
> goes to which?
>
> for a start, we should create a branch off latest main and create the necessary
> claude.md and agents.md first. then a separate branch off latest main and PR for
> scaffolding the basics like openapi, swagger etc for other devs to collab in
> future. and should we also set the dockers as well in a separate PRs?

## Follow-up decisions supplied by the team

Asked as multiple-choice; answers given by the team, not proposed by the tool:

- OpenAPI approach — team noted they do not yet know OpenAPI and asked for a
  recommendation.
- Branch base — "Branch off main and include supplier-service anyway."
- AWS compute target — "Defer — scaffold Docker + CI only for now."

## Key responses

The tool wrote the root and per-service `AGENTS.md`/`CLAUDE.md` pair, the
disclosure scaffolding (`ai/usage-log.md`, `ai/prompts/`), and `ai/decisions.md`
seeded with the stack choices the team had already made. It ran a self-audit
against CS3219 Appendix 2 and removed several passages in which it had authored
design decisions (order state-transition structure, credit idempotency
placement, a shared API error envelope, an admin-bootstrap constraint) rather
than leaving them to the owning developer.
