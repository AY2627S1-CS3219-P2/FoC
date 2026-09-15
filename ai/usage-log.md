# AI Usage Log

Required by CS3219 Appendix 2: *"Maintain a log, `/ai/usage-log.md`, in the
repository with timestamps, prompts, and usage scenarios."* This log is shared —
every team member appends to it, and so does every AI tool that changes files.

The consolidated AI Use Summary required as the README's last section is
generated from this log, so keep entries accurate rather than tidy. What may and
may not be delegated to an AI tool is defined in **§1 of the root `AGENTS.md`**
— that is the single copy; do not restate it here.

## How to add an entry

Append one row per working session to the table under [Entries](#entries),
newest at the bottom. That is the only table in this file.

| Column | What goes in it |
| --- | --- |
| Date/Time | ISO 8601 local, e.g. `2026-09-15T18:40` |
| Member | The human accountable. An agent fills this from `git config user.name` |
| Tool / model | e.g. `Claude Code (Opus 5)`, `Codex (GPT-5)` |
| Mode | `generate`, `refactor`, `debug`, `explain`, `docs`, `test`, or `handed-back` |
| Scope | Files or areas touched, and what changed |
| Prompt | Verbatim, or a link to `ai/prompts/<date>-<slug>.md` |
| Human review | What you checked and verified — `PENDING` until a human has |

**`handed-back`** records a session where root `AGENTS.md` §1 stopped the tool
from doing something. Log these even when no file changed: being able to show
what AI was *not* allowed to write is the strongest evidence the policy was
followed, and there is a 10-minute open Q&A at D4.

**Prompts.** Record the prompt as actually typed, not a cleaned-up version — the
policy asks for "the exact prompts", and a reconstruction that reads better than
the original is itself a disclosure problem. If the prompt contains a newline or
a `|`, or runs past ~200 characters, put the full text in
`ai/prompts/<date>-<slug>.md` and link it. Never reflow or trim a prompt to fit
the table.

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

| Date/Time | Member | Tool / model | Mode | Scope | Prompt | Human review |
| --- | --- | --- | --- | --- | --- | --- |
| 2026-09-15T18:40 | twhjames | Claude Code (Opus 5) | generate | Root + per-service `AGENTS.md`/`CLAUDE.md`, `ai/usage-log.md`, `ai/decisions.md`, `.gitignore` — agent-instruction scaffolding, no application code | [2026-09-15-agent-instructions.md](prompts/2026-09-15-agent-instructions.md) | PENDING |
