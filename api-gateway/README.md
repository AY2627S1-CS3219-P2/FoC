# API Gateway

Single public entry point for FoC. Verifies access tokens, replaces client-supplied
claim headers with verified ones, forwards to the internal services, and serves
the built frontend.

See [`AGENTS.md`](AGENTS.md) for routes and local setup, and
[`../ai/decisions/D-010-api-gateway-auth.md`](../ai/decisions/D-010-api-gateway-auth.md)
for the design it implements.
