# API Gateway

Single public entry point for FoC's API. Verifies access tokens, replaces
client-supplied claim headers with verified ones, and forwards to the internal
services. It serves no pages: the frontend is deployed separately (D-033).

See [`AGENTS.md`](AGENTS.md) for routes and local setup, and
[`../ai/decisions/D-010-api-gateway-auth.md`](../ai/decisions/D-010-api-gateway-auth.md)
for the design it implements.
