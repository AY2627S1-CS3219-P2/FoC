// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5), date: 2026-09-19
// Scope: Request-scoped carriage of the verified identity.
// Author review: PENDING — <reviewer to complete>

package proxy

import "context"

// contextKey is unexported so no other package can write an Identity into a
// request context. Only the gateway's own auth middleware can establish one,
// which is what makes the injected headers trustworthy (D-022).
type contextKey struct{}

// WithIdentity returns a context carrying the verified identity.
func WithIdentity(ctx context.Context, id Identity) context.Context {
	return context.WithValue(ctx, contextKey{}, id)
}

// IdentityFrom returns the verified identity, if the request carries one.
// A false result means the request was not authenticated, and the proxy
// injects no claim headers at all rather than empty ones.
func IdentityFrom(ctx context.Context) (Identity, bool) {
	id, ok := ctx.Value(contextKey{}).(Identity)
	return id, ok
}
