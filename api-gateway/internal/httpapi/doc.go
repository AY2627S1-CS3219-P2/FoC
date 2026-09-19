// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5), date: 2026-09-19
// Scope: Package placeholder only — no implementation.
// Author review: PENDING — <reviewer to complete>

// Package httpapi will hold the gateway's router and its transport-only
// handlers: the auth routes it terminates (login, refresh, logout) and the
// catch-all that hands everything else to internal/proxy.
//
// EMPTY ON PURPOSE. Those route paths and payloads are an API interface, and
// D-005 makes api/openapi.yaml the place they are decided — by a human, then
// generated into internal/gen/ (root AGENTS.md §8).
package httpapi
