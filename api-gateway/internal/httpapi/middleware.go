// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5), date: 2026-09-19
// Scope: Bearer-token middleware — verifies the access token and establishes
//   the request identity the proxy injects.
// Author review: Nigeltzy - I checked the output of this generated code and asked for
// some explanation as this was generated alongside the proxy package that I
// had guided the AI to create.

package httpapi

import (
	"errors"
	"log"
	"net/http"
	"strings"

	"foc/api-gateway/internal/auth"
	"foc/api-gateway/internal/proxy"
)

// bearerPrefix is the Authorization scheme RequireToken accepts.
const bearerPrefix = "Bearer "

// RequireToken verifies the bearer access token and puts the resulting
// identity on the request context, where the proxy reads it to set the claim
// headers. It answers 401 when the token is missing or fails verification,
// and 503 when the signing keys cannot be fetched.
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
					// AI-generated (edited by nigeltzy).
					log.Printf("httpapi: %s %s: %v", r.Method, r.URL.Path, err)
					writeError(w, http.StatusServiceUnavailable, "cannot verify tokens right now")
				case errors.Is(err, auth.ErrExpired):
					writeError(w, http.StatusUnauthorized, "token expired")
				case errors.Is(err, auth.ErrWrongType):
					writeError(w, http.StatusUnauthorized, "refresh token presented where an access token is required")
				default:
					// Malformed and bad-signature tokens get the same message, so the
					// response does not reveal which check failed.
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
