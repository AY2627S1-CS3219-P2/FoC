// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5), date: 2026-09-19
// Scope: Package placeholder only — no implementation.
// Author review: PENDING — <reviewer to complete>

// Package revocation will answer "is this token still good?" against the
// Redis-backed blocklist and suspension keys (D-013, D-014).
//
// OWNERSHIP IS SETTLED: D-017 gives this Redis to the gateway exclusively. It
// is a separate datastore from the User DB and holds only revocation state —
// the jti blocklist and suspended:<uid> keys. No credentials, no refresh
// tokens, no profile data, and no other service gets the connection string.
//
// STILL EMPTY, for a different reason. D-014 as drawn has user-service writing
// those keys itself, which D-017 no longer permits, so logout and suspension
// now need user-service to ASK the gateway to revoke. That inbound call is an
// API nobody has specced (D-005), and its shape decides this package's — so it
// waits for the spec rather than guessing at one.
//
// Reading the store (is this jti blocklisted? is this sub suspended before
// iat?) is unblocked and can be built now.
package revocation
