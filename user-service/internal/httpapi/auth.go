// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-18
// Scope: Added JWT verification middleware and session invalidation interfaces.
// Author review: COMPLETED BY ZI YANG

package httpapi

import (
	"context"
	"net/http"
	"strings"

	"foc/user-service/internal/user"
	"github.com/google/uuid"
)

type principalContextKey struct{}

// Principal identifies the account authenticated by a verified JWT.
type Principal struct {
	UserID uuid.UUID
	Role   user.AccountRole
}

// TokenVerifier verifies a JWT and returns its authenticated principal.
// Claims and signing policy belong to the injected implementation.
type TokenVerifier interface {
	Verify(ctx context.Context, rawToken string) (Principal, error)
}

// SessionInvalidator defines the boundary used to invalidate all active JWT
// sessions for logout or account suspension.
type SessionInvalidator interface {
	InvalidateUserSessions(ctx context.Context, userID uuid.UUID) error
}

// RequireJWT protects an HTTP handler with an injected JWT verifier.
func RequireJWT(verifier TokenVerifier) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if verifier == nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "authentication unavailable"})
				return
			}

			rawToken, ok := bearerToken(r.Header.Get("Authorization"))
			if !ok {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "authentication required"})
				return
			}

			principal, err := verifier.Verify(r.Context(), rawToken)
			if err != nil {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "authentication required"})
				return
			}

			requestContext := context.WithValue(r.Context(), principalContextKey{}, principal)
			next.ServeHTTP(w, r.WithContext(requestContext))
		})
	}
}

// PrincipalFromContext returns the authenticated principal attached by
// RequireJWT.
func PrincipalFromContext(ctx context.Context) (Principal, bool) {
	principal, ok := ctx.Value(principalContextKey{}).(Principal)
	return principal, ok
}

func bearerToken(header string) (string, bool) {
	scheme, token, ok := strings.Cut(header, " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") || token == "" || strings.Contains(token, " ") {
		return "", false
	}
	return token, true
}
