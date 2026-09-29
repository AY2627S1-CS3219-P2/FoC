// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-29
// Scope: Added typed request identity and request-ID context values for gRPC handlers.
// Author review: ZI YANG - validated correctness

// Package interceptors provides the user-service unary gRPC interceptor chain.
package interceptors

import (
	"context"

	"foc/user-service/internal/gen/user/v1"

	"github.com/google/uuid"
)

// Principal is the gateway-verified identity and raw session credential made
// available to RPC adapters.
type Principal struct {
	UserID        uuid.UUID
	Role          userv1.AccountRole
	Authorization string
}

type principalContextKey struct{}
type requestIDContextKey struct{}

// PrincipalFromContext returns transport metadata extracted for the RPC.
func PrincipalFromContext(ctx context.Context) (Principal, bool) {
	principal, ok := ctx.Value(principalContextKey{}).(Principal)
	return principal, ok
}

// RequestIDFromContext returns the transport-generated request identifier.
func RequestIDFromContext(ctx context.Context) string {
	requestID, _ := ctx.Value(requestIDContextKey{}).(string)
	return requestID
}
