// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5), date: 2026-09-19
// Scope: Package placeholder only — no implementation. Doc comment rewritten
//   after D-020 superseded D-017.
// Author review: PENDING — <reviewer to complete>

// Package revocation will answer "is this token still good?" against the
// Redis-backed blocklist and suspension keys (D-013, D-014).
//
// THIS GATEWAY IS A READER, NOT A WRITER. D-020 supersedes D-017: user-service
// is the sole writer to the blocklist, and nothing in this package writes to
// Redis. D-017 had given the store to the gateway outright, which is no longer
// the decision.
//
// That also removes what used to block this package. D-014 always drew
// user-service writing the keys itself, and under D-020 it may, so logout and
// suspension need no inbound "please revoke" API on the gateway — and the spec
// this package was waiting on does not need to exist.
//
// What it reads, per D-014:
//
//   - the jti blocklist — is this token's jti present?
//   - the suspension keys — is this sub suspended, and was the token issued
//     before the suspension timestamp?
//
// If Redis cannot be reached, the request is rejected rather than allowed
// through. Failing open would make revocation advisory.
//
// STILL EMPTY, for two smaller reasons. No Redis client is in go.mod, and
// adding a dependency is called out in root AGENTS.md §2. The exact key
// spelling is also unsettled: the team diagram writes suspended:<uid>, while
// user-service's own AGENTS.md writes suspended:uid:<uuid> and pins the key's
// TTL to the access-token lifetime. That document is not committed to any
// branch yet, so it is not yet something to build against (§1) — the two
// spellings need reconciling into one recorded answer first.
package revocation
