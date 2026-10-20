// AI Assistance Disclosure:
// Tool: Claude Code (model: Sonnet 5.5), date: 2026-10-20
// Scope: The verified caller and the verified token carried on a request's
//   context, for the role check and for forwarding the token to order-service.
// Author review: PENDING — <reviewer to complete>

package auth

import (
	"context"

	"github.com/google/uuid"
)

// Caller is the account a verified token names (D-041).
type Caller struct {
	UserID uuid.UUID
	// Role is the token's role claim: STUDENT or ADMIN (D-019).
	Role string
}

type callerKey struct{}

type tokenKey struct{}

// ContextWithCaller returns ctx carrying the verified caller.
func ContextWithCaller(ctx context.Context, c Caller) context.Context {
	return context.WithValue(ctx, callerKey{}, c)
}

// CallerFromContext returns the caller ContextWithCaller put on ctx, if any.
func CallerFromContext(ctx context.Context) (Caller, bool) {
	c, ok := ctx.Value(callerKey{}).(Caller)
	return c, ok
}

// ContextWithToken returns ctx carrying the caller's verified access token, so
// a call made on the caller's behalf can forward it.
func ContextWithToken(ctx context.Context, token string) context.Context {
	return context.WithValue(ctx, tokenKey{}, token)
}

// TokenFromContext returns the token ContextWithToken put on ctx, if any.
func TokenFromContext(ctx context.Context) (string, bool) {
	t, ok := ctx.Value(tokenKey{}).(string)
	return t, ok && t != ""
}
