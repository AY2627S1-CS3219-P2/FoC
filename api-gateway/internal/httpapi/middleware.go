// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5), date: 2026-09-19
// Scope: Bearer-token middleware — verifies the access token and establishes
//   the request identity the proxy injects.
// Author review: Nigeltzy - I checked the output of this generated code and asked for
// some explanation as this was generated alongside the proxy package that I
// had guided the AI to create.

package httpapi

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5/middleware"

	"foc/api-gateway/internal/auth"
	"foc/api-gateway/internal/proxy"
)

// bearerPrefix is the Authorization scheme RequireToken accepts.
const bearerPrefix = "Bearer "

// RevocationChecker reports whether user-service has revoked a verified
// token: logged out, or issued at or before its account's suspension (D-054).
// internal/revocation's Checker is the one NewRouter gets outside tests. An
// error means the check could not be made.
type RevocationChecker interface {
	Revoked(ctx context.Context, tokenID, subject string, issuedAt time.Time) (bool, error)
}

// RequireToken verifies the bearer access token, checks it has not been
// revoked, and puts the resulting identity on the request context, where the
// proxy reads it to set the claim headers. It answers 401 when the token is
// missing, fails verification or is revoked, and 503 when the signing keys
// cannot be fetched or the revocation check cannot be made (D-077).
func RequireToken(verifier *auth.Verifier, revocations RevocationChecker) func(http.Handler) http.Handler {
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

			// Only a token whose signature, exp and typ have passed reaches
			// Redis, so junk tokens cost no lookup. A refusal logs the
			// request's id and the token's sub, never the token itself.
			revoked, err := revocations.Revoked(r.Context(), claims.ID, claims.Subject, claims.IssuedAt)
			switch {
			case err != nil:
				// Fail closed: a token that cannot be checked is not let
				// through. 503, not 401: the frontend answers a 401 by
				// refreshing, and refresh needs Redis too, so an outage
				// reported as 401 would sign every user out.
				log.Printf("httpapi: %s %s request_id=%q sub=%q: revocation check failed: %v",
					r.Method, r.URL.Path, middleware.GetReqID(r.Context()), claims.Subject, err)
				writeError(w, http.StatusServiceUnavailable, "cannot check tokens right now")
				return
			case revoked:
				// The same message as a bad signature, so the response does
				// not say which check failed.
				log.Printf("httpapi: %s %s request_id=%q sub=%q: token revoked",
					r.Method, r.URL.Path, middleware.GetReqID(r.Context()), claims.Subject)
				writeError(w, http.StatusUnauthorized, "invalid token")
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
