<!--
AI Assistance Disclosure:
Tool: Claude Code (model: Opus 5.5), date: 2026-11-05
Scope: How to run notification-service.
Author review: PENDING — <reviewer to complete>
-->

# notification-service

In-app notifications for Friend on Campus (backlog F5). It reads order lifecycle
events from its own queue and stores one notification per recipient per event.
Rules and boundaries are in [AGENTS.md](AGENTS.md); the API is
[api/openapi.yaml](api/openapi.yaml).

## Run it

`docker compose up` from the repository root starts it with its database. It
listens on 8086 inside the Compose network and has no host port; browsers reach
it through the gateway at `/api/notifications`.

For a host run, set the variables in [.env.example](.env.example) and run
`go run ./cmd/api` from this folder.

## If it is stopped

Nothing else notices. Events keep queuing in `notification.errands` and are
processed when it comes back. Past 100,000 queued messages or 256 MiB the oldest
are dropped, so those notifications are lost, and publishers are never refused.
While it is down, `/api/notifications` through the gateway fails and the
frontend's Notifications screen says so; every other screen works.

## Failed events

An event that cannot be parsed or is missing a required field goes straight to
`notification.errands.dlq`. Any other failure, such as the database being down,
is retried, and the queue dead-letters the event after 5 deliveries. Dead-lettered
events stay readable in that queue for diagnosis and replay (F6.2).
