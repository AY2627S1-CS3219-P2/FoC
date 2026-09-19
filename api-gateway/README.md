# API Gateway

Single public entry point for FoC. Verifies access tokens, checks revocation,
translates claims into headers, and forwards to the internal services.

Currently a **scaffold**: it starts, serves `/healthz`, and shuts down cleanly.
See [`AGENTS.md`](AGENTS.md) for what is deliberately unbuilt and why, and
[`../ai/decisions/D-010-api-gateway-auth.md`](../ai/decisions/D-010-api-gateway-auth.md)
for the architecture it implements.
