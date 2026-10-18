// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-14
// Scope: The middleware every order-service request passes: request id (D-047),
//   access log (NFR7.1), panic recovery, body limit, write deadline and the
//   caller's access token, verified here and nowhere else (D-054, D-056).
// Author review: James Teo — reviewed and verified.

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
	"unicode/utf8"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"

	"foc/order-service/internal/auth"
	"foc/order-service/internal/order"
)

const (
	headerRequestID     = "X-Request-Id"
	headerAuthorization = "Authorization"

	// maxRequestIDBytes caps a kept X-Request-Id in bytes. The relay sends the
	// id as the AMQP message's CorrelationId, a short string of at most 255
	// bytes, so a longer id would fail its event's publish on every retry.
	maxRequestIDBytes = 255
	// maxBodyBytes caps a POST or PATCH body, comfortably above the largest
	// valid one (a 280-character item and a 140-character note).
	maxBodyBytes = 16 << 10
	// writeTimeout bounds a POST or PATCH so it ends before the gateway's
	// 15-second response timeout gives up on it.
	writeTimeout = 10 * time.Second
)

type callerKey struct{}

type requestInfoKey struct{}

// requestInfo is how inner middleware reports back to the access log that
// wraps it: authenticate fills userID once it has verified the caller.
type requestInfo struct {
	userID string
}

// requestID keeps the client's X-Request-Id as the correlation id when
// validRequestID accepts it and otherwise makes one, so every log line, credit
// call and event of the request carries the same id (D-047, NFR7.1). The id is
// echoed in the response.
func requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := clientRequestID(r.Header)
		if id == "" {
			id = uuid.NewString()
		}
		w.Header().Set(headerRequestID, id)
		next.ServeHTTP(w, r.WithContext(order.WithCorrelationID(r.Context(), id)))
	})
}

// clientRequestID returns the client's X-Request-Id, or "" when it is missing,
// sent more than once, or refused by validRequestID.
func clientRequestID(h http.Header) string {
	vals := h.Values(headerRequestID)
	if len(vals) == 1 && validRequestID(vals[0]) {
		return vals[0]
	}
	return ""
}

// validRequestID is the one rule an X-Request-Id must pass to be kept, both as
// the correlation id here and as POST /'s duplicate-submission key in
// CreateErrand, so a kept key is always the request's correlation id (D-047).
// The id must be UTF-8, which PostgreSQL text needs to store the key, within
// the contract's maxLength in characters, and within maxRequestIDBytes.
func validRequestID(v string) bool {
	return v != "" && utf8.ValidString(v) &&
		utf8.RuneCountInString(v) <= maxRequestKeyLength && len(v) <= maxRequestIDBytes
}

// accessLog writes one structured line per request (NFR7.1).
func accessLog(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			info := &requestInfo{}
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r.WithContext(context.WithValue(r.Context(), requestInfoKey{}, info)))

			status := ww.Status()
			if status == 0 {
				// Nothing was written, so net/http sends 200.
				status = http.StatusOK
			}
			attrs := []slog.Attr{
				slog.String("request_id", order.CorrelationID(r.Context())),
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", status),
				slog.Float64("duration_ms", float64(time.Since(start))/float64(time.Millisecond)),
			}
			if info.userID != "" {
				attrs = append(attrs, slog.String("user_id", info.userID))
			}
			log.LogAttrs(r.Context(), slog.LevelInfo, "http request", attrs...)
		})
	}
}

// recoverPanics turns a panic into a 500 and an ERROR log line, so one bad
// request cannot take the process down.
func recoverPanics(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				rec := recover()
				if rec == nil {
					return
				}
				if err, ok := rec.(error); ok && errors.Is(err, http.ErrAbortHandler) {
					// net/http's deliberate way to abort a response; let it
					// through as chi's Recoverer does.
					panic(rec)
				}
				log.LogAttrs(r.Context(), slog.LevelError, "panic serving request",
					slog.String("request_id", order.CorrelationID(r.Context())),
					slog.String("panic", fmt.Sprint(rec)),
					slog.String("stack", string(debug.Stack())))
				if !responseStarted(w) {
					writeError(w, http.StatusInternalServerError, msgInternal)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// responseStarted reports whether a status has already gone out on w, after
// which a 500 can no longer be sent.
func responseStarted(w http.ResponseWriter) bool {
	sw, ok := w.(interface{ Status() int })
	return ok && sw.Status() != 0
}

// limitBody caps POST and PATCH bodies at maxBodyBytes: a body that turns out
// longer fails to decode with a 413 (decodeBody). The cap bites only as a body
// is read, so the body-less actions, whose contract lists no 413, ignore
// whatever body they are sent.
func limitBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isWrite(r.Method) {
			r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
		}
		next.ServeHTTP(w, r)
	})
}

// writeDeadline gives POST and PATCH writeTimeout to finish, so a slow
// dependency fails the request here instead of at the gateway.
func writeDeadline(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isWrite(r.Method) {
			next.ServeHTTP(w, r)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), writeTimeout)
		defer cancel()
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func isWrite(method string) bool {
	return method == http.MethodPost || method == http.MethodPatch
}

// TokenVerifier checks an access token's signature, expiry and type (D-056).
// *auth.Verifier satisfies it.
type TokenVerifier interface {
	Verify(ctx context.Context, token string) (auth.Claims, error)
}

// RevocationChecker reports whether user-service revoked a verified token
// (D-054). *auth.Revocations satisfies it; an error means Redis did not answer.
type RevocationChecker interface {
	Revoked(ctx context.Context, c auth.Claims) (bool, error)
}

// authenticate verifies the caller's access token itself (D-056) and puts the
// caller, and the token for forwarding to credit-service, in the request
// context. It is the only reader of the Authorization header, and it ignores
// any X-User-Id or X-User-Role. GET /health skips it.
//
// A missing, malformed, invalid, expired or revoked token is a 401, and
// nothing else is, because the frontend treats a 401 as an expired token.
// When user-service's keys cannot be loaded nothing can be verified, so the
// answer is 503. When Redis does not answer, the revocation check is skipped
// and the skip is logged (D-056).
func authenticate(v TokenVerifier, rev RevocationChecker, log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if isPublicRoute(r) {
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
				log.ErrorContext(ctx, "cannot load user-service's signing keys; refusing the request",
					"request_id", order.CorrelationID(ctx), "err", err)
				writeError(w, http.StatusServiceUnavailable, msgKeysUnavailable)
				return
			}
			role := order.Role(claims.Role)
			if err != nil || (role != order.RoleStudent && role != order.RoleAdmin) {
				writeError(w, http.StatusUnauthorized, msgUnauthenticated)
				return
			}
			revoked, err := rev.Revoked(ctx, claims)
			switch {
			case err != nil:
				log.ErrorContext(ctx, "revocation check skipped: Redis did not answer",
					"request_id", order.CorrelationID(ctx), "user_id", claims.UserID.String(), "err", err)
			case revoked:
				writeError(w, http.StatusUnauthorized, msgUnauthenticated)
				return
			}
			c := order.Caller{UserID: claims.UserID, Role: role}
			if info, ok := ctx.Value(requestInfoKey{}).(*requestInfo); ok {
				info.userID = c.UserID.String()
			}
			ctx = auth.ContextWithToken(context.WithValue(ctx, callerKey{}, c), token)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// isPublicRoute matches GET /health exactly, the one route that needs no token.
func isPublicRoute(r *http.Request) bool {
	return r.Method == http.MethodGet && routingPath(r) == "/health"
}

// routingPath is the path chi routes on: the escaped form when the URL has one.
func routingPath(r *http.Request) string {
	if r.URL.RawPath != "" {
		return r.URL.RawPath
	}
	return r.URL.Path
}

// bearerToken returns the token of exactly one "Authorization: Bearer <token>"
// header. The scheme is case-insensitive (RFC 9110 §11.1).
func bearerToken(h http.Header) (string, bool) {
	values := h.Values(headerAuthorization)
	if len(values) != 1 {
		return "", false
	}
	scheme, token, ok := strings.Cut(values[0], " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") || token == "" || strings.ContainsAny(token, " \t") {
		return "", false
	}
	return token, true
}

// callerOf returns the caller put in r's context, answering 401 when there is
// none. Only the route authenticate skips can lack one.
func callerOf(w http.ResponseWriter, r *http.Request) (order.Caller, bool) {
	c, ok := r.Context().Value(callerKey{}).(order.Caller)
	if !ok {
		writeError(w, http.StatusUnauthorized, msgUnauthenticated)
	}
	return c, ok
}
