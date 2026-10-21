// AI Assistance Disclosure:
// Tool: Claude Code (model: Sonnet 5.5), date: 2026-10-20
// Scope: The token check on supplier writes: the bearer token is verified and
//   its revocation checked here, and the admin role comes from the verified
//   token instead of a header (D-041, D-054). Modelled on order-service's
//   authenticate middleware.
// Author review: PENDING — <reviewer to complete>

package httpapi

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strings"

	"foc/supplier-service/internal/auth"
)

// Messages for the answers a refused write gets. The 401 and 503 texts are
// order-service's, so the frontend sees one wording.
const (
	msgUnauthenticated = "missing, invalid or expired access token"
	msgKeysUnavailable = "sign-in cannot be checked right now; please try again"
	msgAdminRequired   = "admin role required"
)

const (
	roleAdmin   = "ADMIN"
	roleStudent = "STUDENT"
)

// TokenVerifier verifies an access token. *auth.Verifier satisfies it.
type TokenVerifier interface {
	Verify(ctx context.Context, token string) (auth.Claims, error)
}

// RevocationChecker reports whether user-service revoked a verified token
// (D-054). *auth.Revocations satisfies it.
type RevocationChecker interface {
	Revoked(ctx context.Context, c auth.Claims) (bool, error)
}

// authenticate verifies the caller's access token itself (D-041) and puts the
// caller and the token in the request context. It is the only reader of the
// Authorization header, and it ignores any X-User-Id or X-User-Role.
//
// A missing, malformed, invalid, expired or revoked token is a 401, and
// nothing else is, because the frontend answers a 401 by refreshing the token
// and retrying once. A token whose role is neither STUDENT nor ADMIN is a 401
// too: user-service never issues one.
//
// When user-service's keys cannot be loaded, or the revocation keys cannot be
// read (Redis down, or answering with an error), nothing can be checked, so
// the write is refused with a 503 rather than let through unchecked.
func authenticate(v TokenVerifier, rev RevocationChecker) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			token, ok := bearerToken(r.Header)
			if !ok {
				refuse(w, r, http.StatusUnauthorized, msgUnauthenticated, "no single Bearer token in Authorization")
				return
			}
			claims, err := v.Verify(ctx, token)
			if errors.Is(err, auth.ErrKeysUnavailable) {
				log.Printf("%s %s refused: cannot load user-service's signing keys: %v", r.Method, r.URL.Path, err)
				writeError(w, http.StatusServiceUnavailable, msgKeysUnavailable)
				return
			}
			if err != nil {
				refuse(w, r, http.StatusUnauthorized, msgUnauthenticated, err.Error())
				return
			}
			if claims.Role != roleAdmin && claims.Role != roleStudent {
				refuse(w, r, http.StatusUnauthorized, msgUnauthenticated, "token has no known role")
				return
			}

			revoked, err := rev.Revoked(ctx, claims)
			switch {
			case err != nil && ctx.Err() != nil:
				// The client hung up during the lookup; nothing is wrong with
				// Redis, so nothing is logged beyond the access log line.
				writeError(w, http.StatusServiceUnavailable, msgKeysUnavailable)
				return
			case errors.Is(err, auth.ErrRedisUnreachable):
				log.Printf("%s %s refused for %s: Redis could not be reached: %v", r.Method, r.URL.Path, claims.UserID, err)
				writeError(w, http.StatusServiceUnavailable, msgKeysUnavailable)
				return
			case err != nil:
				log.Printf("%s %s refused for %s: cannot read the revocation keys: %v", r.Method, r.URL.Path, claims.UserID, err)
				writeError(w, http.StatusServiceUnavailable, msgKeysUnavailable)
				return
			case revoked:
				refuse(w, r, http.StatusUnauthorized, msgUnauthenticated, "token revoked for "+claims.UserID.String())
				return
			}

			ctx = auth.ContextWithCaller(ctx, auth.Caller{UserID: claims.UserID, Role: claims.Role})
			ctx = auth.ContextWithToken(ctx, token)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// requireAdmin lets a request through only when the verified caller authenticate
// put in the context is an ADMIN. It must run after authenticate.
func requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		caller, ok := auth.CallerFromContext(r.Context())
		if !ok {
			// Only a router that skipped authenticate gets here.
			refuse(w, r, http.StatusUnauthorized, msgUnauthenticated, "no verified caller on the request")
			return
		}
		if caller.Role != roleAdmin {
			refuse(w, r, http.StatusForbidden, msgAdminRequired, "role "+caller.Role+" for "+caller.UserID.String())
			return
		}
		log.Printf("admin write: %s %s by %s", r.Method, r.URL.Path, caller.UserID)
		next.ServeHTTP(w, r)
	})
}

// refuse answers a write with status and msg, and logs the reason, which the
// client is not told. The token is never logged.
func refuse(w http.ResponseWriter, r *http.Request, status int, msg, reason string) {
	log.Printf("%s %s refused with %d: %s", r.Method, r.URL.Path, status, reason)
	writeError(w, status, msg)
}

// bearerToken returns the token of exactly one "Authorization: Bearer <token>"
// header. The scheme is case-insensitive (RFC 9110 §11.1); a token with
// whitespace in it is refused.
func bearerToken(h http.Header) (string, bool) {
	values := h.Values("Authorization")
	if len(values) != 1 {
		return "", false
	}
	scheme, token, ok := strings.Cut(values[0], " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") || token == "" || strings.ContainsAny(token, " \t") {
		return "", false
	}
	return token, true
}
