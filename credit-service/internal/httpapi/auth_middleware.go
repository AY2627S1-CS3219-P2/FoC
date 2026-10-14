// AI Assistance Disclosure:
// Tool: Codex (model: GPT-5), date: 2026-10-14
// Scope: Added Bearer-token middleware for credit-service routes per D-041.
// Author review: PENDING - reviewer to complete

package httpapi

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strings"

	"foc/credit-service/internal/auth"
)

type contextKey string

const (
	bearerPrefix = "Bearer "
	identityKey  = contextKey("identity")
)

// Identity is the verified caller identity extracted from an access token.
type Identity struct {
	UserID string
	Role   string
}

// WithIdentity returns a context carrying a verified caller identity.
func WithIdentity(ctx context.Context, identity Identity) context.Context {
	return context.WithValue(ctx, identityKey, identity)
}

// IdentityFromContext returns the verified caller identity from ctx.
func IdentityFromContext(ctx context.Context) (Identity, bool) {
	identity, ok := ctx.Value(identityKey).(Identity)
	return identity, ok
}

// RequireToken verifies a Bearer access token before serving a protected route.
func RequireToken(verifier *auth.Verifier) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			header := r.Header.Get("Authorization")
			if !strings.HasPrefix(header, bearerPrefix) {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing or malformed Authorization header"})
				return
			}
			token := strings.TrimSpace(strings.TrimPrefix(header, bearerPrefix))

			claims, err := verifier.Verify(r.Context(), token)
			if err != nil {
				switch {
				case errors.Is(err, auth.ErrKeysUnavailable):
					log.Printf("httpapi: %s %s: %v", r.Method, r.URL.Path, err)
					writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "cannot verify tokens right now"})
				case errors.Is(err, auth.ErrExpired):
					writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "token expired"})
				case errors.Is(err, auth.ErrWrongType):
					writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "refresh token presented where an access token is required"})
				default:
					writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid token"})
				}
				return
			}

			ctx := WithIdentity(r.Context(), Identity{
				UserID: claims.Subject,
				Role:   claims.Role,
			})
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
