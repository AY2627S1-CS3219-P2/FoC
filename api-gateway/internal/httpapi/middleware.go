// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5), date: 2026-09-19
// Scope: Bearer-token middleware — verifies the access token and establishes
//   the request identity the proxy injects.
// Author review: PENDING — <reviewer to complete>

package httpapi

import (
	"errors"
	"net/http"
	"strings"

	"foc/api-gateway/internal/auth"
	"foc/api-gateway/internal/proxy"
)

// bearerPrefix is the scheme the frontend sends (frontend/src/lib/http.ts).
const bearerPrefix = "Bearer "

// RequireToken verifies the access token and puts the resulting identity on
// the request context.
//
// It is the ONLY way an Identity comes into existence — proxy's context key is
// unexported — so a request that reaches a downstream service with claim
// headers set has necessarily passed through here (D-022).
func RequireToken(verifier *auth.Verifier) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			header := r.Header.Get("Authorization")
			if !strings.HasPrefix(header, bearerPrefix) {
				writeError(w, http.StatusUnauthorized, "missing or malformed Authorization header")
				return
			}
			token := strings.TrimSpace(strings.TrimPrefix(header, bearerPrefix))

			claims, err := verifier.Verify(r.Context(), token)
			if err != nil {
				switch {
				case errors.Is(err, auth.ErrKeysUnavailable):
					// The gateway cannot verify anything right now. This is
					// the gateway's failure, not the caller's, and it must
					// not be reported as a bad token.
					writeError(w, http.StatusServiceUnavailable, "cannot verify tokens right now")
				case errors.Is(err, auth.ErrExpired):
					writeError(w, http.StatusUnauthorized, "token expired")
				case errors.Is(err, auth.ErrWrongType):
					writeError(w, http.StatusUnauthorized, "refresh token presented where an access token is required")
				default:
					// Malformed and bad-signature deliberately collapse into
					// one message: telling a caller which one it was helps
					// nobody but an attacker probing the format.
					writeError(w, http.StatusUnauthorized, "invalid token")
				}
				return
			}

			ctx := proxy.WithIdentity(r.Context(), proxy.Identity{
				UserID: claims.Subject,
				Role:   claims.Role,
			})
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
