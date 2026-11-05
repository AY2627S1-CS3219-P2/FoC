// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-11-05
// Scope: Access log, panic recovery and token authentication, adapted from
//   order-service/internal/httpapi/middleware.go (D-041, D-054, D-056).
// Author review: PENDING — <reviewer to complete>

package httpapi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"

	"foc/notification-service/internal/auth"
)

type callerKey struct{}

// TokenVerifier checks an access token's signature, expiry and type.
// *auth.Verifier satisfies it.
type TokenVerifier interface {
	Verify(ctx context.Context, token string) (auth.Claims, error)
}

// RevocationChecker reports whether user-service revoked a verified token
// (D-054). *auth.Revocations satisfies it.
type RevocationChecker interface {
	Revoked(ctx context.Context, c auth.Claims) (bool, error)
}

// accessLog writes one structured line per request.
func accessLog(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)
			status := ww.Status()
			if status == 0 {
				status = http.StatusOK
			}
			log.LogAttrs(r.Context(), slog.LevelInfo, "http request",
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", status),
				slog.Float64("duration_ms", float64(time.Since(start))/float64(time.Millisecond)))
		})
	}
}

// recoverPanics turns a panic into a 500 and an ERROR log line.
func recoverPanics(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				rec := recover()
				if rec == nil {
					return
				}
				if err, ok := rec.(error); ok && errors.Is(err, http.ErrAbortHandler) {
					panic(rec)
				}
				log.LogAttrs(r.Context(), slog.LevelError, "panic serving request",
					slog.String("panic", fmt.Sprint(rec)),
					slog.String("stack", string(debug.Stack())))
				writeError(w, http.StatusInternalServerError, msgInternal)
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// authenticate verifies the caller's access token itself (D-041) and puts the
// caller's id in the request context. It ignores X-User-Id and X-User-Role.
// GET /health skips it.
//
// A missing, invalid, expired or revoked token is a 401, and nothing else is,
// because the frontend treats a 401 as an expired token. As in order-service
// (D-056), only an unreachable Redis skips the revocation check, and the skip is
// logged; when Redis answers with an error, or the signing keys cannot be
// loaded, the answer is 503.
func authenticate(v TokenVerifier, rev RevocationChecker, log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet && r.URL.Path == "/health" {
				next.ServeHTTP(w, r)
				return
			}
			ctx := r.Context()
			token, ok := bearerToken(r.Header)
			if !ok {
				writeError(w, http.StatusUnauthorized, msgUnauthenticated)
				return
			}
			claims, err := v.Verify(ctx, token)
			if errors.Is(err, auth.ErrKeysUnavailable) {
				log.ErrorContext(ctx, "cannot load user-service's signing keys; refusing the request", "err", err)
				writeError(w, http.StatusServiceUnavailable, msgAuthUnavailable)
				return
			}
			if err != nil {
				writeError(w, http.StatusUnauthorized, msgUnauthenticated)
				return
			}
			revoked, err := rev.Revoked(ctx, claims)
			switch {
			case err != nil && ctx.Err() != nil:
				writeError(w, http.StatusServiceUnavailable, msgAuthUnavailable)
				return
			case errors.Is(err, auth.ErrRedisUnreachable):
				log.ErrorContext(ctx, "revocation check skipped: Redis could not be reached",
					"user_id", claims.UserID.String(), "err", err)
			case err != nil:
				log.ErrorContext(ctx, "cannot read the revocation keys in Redis; refusing the request",
					"user_id", claims.UserID.String(), "err", err)
				writeError(w, http.StatusServiceUnavailable, msgAuthUnavailable)
				return
			case revoked:
				writeError(w, http.StatusUnauthorized, msgUnauthenticated)
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(ctx, callerKey{}, claims.UserID)))
		})
	}
}

// bearerToken returns the token of exactly one "Authorization: Bearer <token>"
// header. The scheme is case-insensitive (RFC 9110 §11.1).
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

// callerOf returns the id authenticate put in r's context, answering 401 when
// there is none.
func callerOf(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, ok := r.Context().Value(callerKey{}).(uuid.UUID)
	if !ok {
		writeError(w, http.StatusUnauthorized, msgUnauthenticated)
	}
	return id, ok
}
