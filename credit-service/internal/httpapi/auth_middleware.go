// AI Assistance Disclosure:
// Tool: Antigravity, date: 2026-10-20
// Scope: Bearer-token authentication middleware verifying RS256 JWTs against JWKS and checking Redis revocation keys per D-041/D-054/D-056.
// Author review: PENDING - reviewer to complete

package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"foc/credit-service/internal/auth"
	"foc/credit-service/internal/gen"

	"github.com/google/uuid"
)

type contextKey string

const (
	bearerPrefix = "Bearer "
	identityKey  = contextKey("identity")
)

// Identity is the verified caller identity extracted from an access token.
type Identity struct {
	UserID uuid.UUID
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

// RequireToken verifies a Bearer access token and checks Redis token revocation.
func RequireToken(verifier *auth.Verifier, rev *auth.Revocations, logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token, ok := bearerToken(r.Header)
			if !ok {
				writeError(w, http.StatusUnauthorized, gen.INVALIDREQUEST, "missing or malformed Authorization header")
				return
			}

			ctx := r.Context()
			claims, err := verifier.Verify(ctx, token)
			if err != nil {
				if errors.Is(err, auth.ErrKeysUnavailable) {
					if logger != nil {
						logger.ErrorContext(ctx, "cannot load user-service's signing keys; refusing request", "err", err)
					}
					writeError(w, http.StatusServiceUnavailable, gen.SERVICEUNAVAILABLE, "cannot verify tokens right now")
					return
				}
				switch {
				case errors.Is(err, auth.ErrExpired):
					writeError(w, http.StatusUnauthorized, gen.INVALIDREQUEST, "token expired")
				case errors.Is(err, auth.ErrWrongType):
					writeError(w, http.StatusUnauthorized, gen.INVALIDREQUEST, "not an access token")
				default:
					writeError(w, http.StatusUnauthorized, gen.INVALIDREQUEST, "invalid token")
				}
				return
			}

			if rev != nil {
				revoked, revErr := rev.Revoked(ctx, claims)
				switch {
				case revErr != nil && ctx.Err() != nil:
					writeError(w, http.StatusServiceUnavailable, gen.SERVICEUNAVAILABLE, "request cancelled during revocation check")
					return
				case errors.Is(revErr, auth.ErrRedisUnreachable):
					if logger != nil {
						logger.WarnContext(ctx, "revocation check skipped: Redis could not be reached", "err", revErr)
					}
				case revErr != nil:
					if logger != nil {
						logger.ErrorContext(ctx, "cannot read revocation keys in Redis", "err", revErr)
					}
					writeError(w, http.StatusServiceUnavailable, gen.SERVICEUNAVAILABLE, "cannot check token revocation right now")
					return
				case revoked:
					writeError(w, http.StatusUnauthorized, gen.INVALIDREQUEST, "token has been revoked")
					return
				}
			}

			next.ServeHTTP(w, r.WithContext(WithIdentity(ctx, Identity{
				UserID: claims.UserID,
				Role:   claims.Role,
			})))
		})
	}
}

func bearerToken(h http.Header) (string, bool) {
	authHdr := h.Get("Authorization")
	if !strings.HasPrefix(authHdr, bearerPrefix) {
		return "", false
	}
	token := strings.TrimSpace(strings.TrimPrefix(authHdr, bearerPrefix))
	return token, token != ""
}
