// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5), date: 2026-09-19
// Scope: Package placeholder only — no implementation.
// Author review: PENDING — <reviewer to complete>

// Package proxy will forward a verified request to the downstream service over
// synchronous REST, translating the token claims into HTTP headers (D-013).
//
// EMPTY ON PURPOSE. Which header names carry the claims is unrecorded — today
// supplier-service reads "X-User-Role" as a self-described interim stand-in
// (supplier-service/internal/middleware/auth.go), and whether that becomes the
// real name is the team's call, not this package's.
package proxy
