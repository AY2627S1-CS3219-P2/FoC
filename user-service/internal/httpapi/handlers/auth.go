// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-21
// Scope: Relocated JWT middleware and authenticated-principal transport boundary with handlers.
// Author review: COMPLETED BY ZI YANG

package handlers

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"foc/user-service/internal/user"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
)

type principalContextKey struct{}

// Principal identifies the account authenticated by a verified JWT.
type Principal struct {
	UserID   uuid.UUID
	Role     user.AccountRole
	JTI      uuid.UUID
	IssuedAt time.Time
}

// TokenVerifier verifies a JWT and returns its authenticated principal.
type TokenVerifier interface {
	Verify(context.Context, string) (Principal, error)
}

// RevocationChecker reports whether a verified access token has been revoked.
type RevocationChecker interface {
	Check(context.Context, Principal) (bool, error)
}

// SessionInvalidator invalidates all active JWT sessions for an account.
type SessionInvalidator interface {
	InvalidateUserSessions(context.Context, uuid.UUID) error
}

// RequireJWT protects an HTTP handler with an injected JWT verifier.
func RequireJWT(verifier TokenVerifier, revocationChecker RevocationChecker, logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if verifier == nil {
				writeError(r.Context(), w, logger, http.StatusInternalServerError, errors.New("token verifier is required"), "authentication unavailable")
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
			if revocationChecker == nil {
				writeError(r.Context(), w, logger, http.StatusInternalServerError, errors.New("revocation checker is required"), "authentication unavailable")
				return
			}
			revoked, err := revocationChecker.Check(r.Context(), principal)
			if err != nil {
				writeError(r.Context(), w, logger, http.StatusInternalServerError, fmt.Errorf("check token revocation: %w", err), "authentication unavailable")
				return
			}
			if revoked {
				// Logged with the account and the request, never the token.
				if logger != nil {
					logger.InfoContext(r.Context(), "revoked access token refused",
						"user_id", principal.UserID, "request_id", middleware.GetReqID(r.Context()))
				}
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "authentication required"})
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), principalContextKey{}, principal)))
		})
	}
}

// PrincipalFromContext returns the principal attached by RequireJWT.
func PrincipalFromContext(ctx context.Context) (Principal, bool) {
	principal, ok := ctx.Value(principalContextKey{}).(Principal)
	return principal, ok
}

func bearerToken(header string) (string, bool) {
	scheme, token, ok := strings.Cut(header, " ")
	return token, ok && strings.EqualFold(scheme, "Bearer") && token != "" && !strings.Contains(token, " ")
}
