// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5), date: 2026-09-19
// Scope: Package placeholder only — no implementation.
// Author review: PENDING — <reviewer to complete>

// Package auth will verify access-token signatures and expose the claims
// D-011 defines: sub, role, jti, exp.
//
// EMPTY ON PURPOSE. Verification needs the JWT signing algorithm and the AT
// lifetime, neither of which is recorded yet (ai/decisions.md, Open table).
// Writing it now would be picking them.
package auth
