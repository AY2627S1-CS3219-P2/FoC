// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-14
// Scope: Carries the request's correlation id in its context, so adapters can
//   forward and log it (D-047, NFR7.1).
// Author review: James Teo — reviewed and verified.

package order

import "context"

type correlationKey struct{}

// WithCorrelationID returns a context carrying id, the request's X-Request-Id.
func WithCorrelationID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, correlationKey{}, id)
}

// CorrelationID returns the id WithCorrelationID stored, or "".
func CorrelationID(ctx context.Context) string {
	id, _ := ctx.Value(correlationKey{}).(string)
	return id
}
