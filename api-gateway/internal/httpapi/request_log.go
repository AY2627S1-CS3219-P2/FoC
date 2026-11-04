// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-11-04
// Scope: The middleware every gateway request passes for logging: its request
//   id, its one JSON access line and panic recovery (D-135).
// Author review: James Teo — reviewed and verified.

package httpapi

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/go-chi/chi/v5/middleware"

	"foc/api-gateway/internal/proxy"
)

type requestInfoKey struct{}

// requestInfo is how RequireToken reports back to the access log that wraps
// it: it fills userID once the caller's token has passed every check.
type requestInfo struct {
	userID string
}

// requestID gives the request its id. A client's X-Request-Id is kept exactly
// as sent, never checked or replaced: order-service uses it as the
// errand-create duplicate key (D-047). Without one the gateway mints a UUID.
// Either way the id becomes the header's only value on the request, so the
// proxy forwards the same id the gateway logs, even when the client sent
// several. It also goes into the context for the gateway's own log lines and
// is returned once in the response.
func requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := clientRequestID(r.Header)
		if id == "" {
			id = newRequestID()
		}
		r.Header.Set(proxy.HeaderRequestID, id)
		w.Header().Set(proxy.HeaderRequestID, id)
		next.ServeHTTP(w, r.WithContext(proxy.WithRequestID(r.Context(), id)))
	})
}

// clientRequestID returns the first non-empty X-Request-Id the client sent,
// or "" when it sent none.
func clientRequestID(h http.Header) string {
	for _, v := range h.Values(proxy.HeaderRequestID) {
		if v != "" {
			return v
		}
	}
	return ""
}

// newRequestID returns a random (version 4) UUID.
func newRequestID() string {
	var b [16]byte
	// crypto/rand.Read never returns an error; it crashes the process instead.
	_, _ = rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40 // version 4
	b[8] = b[8]&0x3f | 0x80 // RFC 9562 variant
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

// accessLog writes one line per request once it has finished: WARN when the
// client hung up, ERROR for a 5xx, INFO otherwise.
func accessLog(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			info := &requestInfo{}
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			// Deferred: ReverseProxy aborts a response whose body breaks off
			// by panicking with http.ErrAbortHandler, and that request still
			// gets its line.
			defer func() {
				status := ww.Status()
				if status == 0 {
					// Nothing was written, so net/http sends 200.
					status = http.StatusOK
				}
				attrs := []slog.Attr{
					slog.String("request_id", proxy.RequestIDFrom(r.Context())),
					slog.String("method", r.Method),
					slog.String("path", r.URL.Path),
					slog.Int("status", status),
					slog.Float64("duration_ms", float64(time.Since(start))/float64(time.Millisecond)),
				}
				if info.userID != "" {
					attrs = append(attrs, slog.String("user_id", info.userID))
				}
				log.LogAttrs(r.Context(), accessLevel(r.Context(), status), "http request", attrs...)
			}()
			next.ServeHTTP(ww, r.WithContext(context.WithValue(r.Context(), requestInfoKey{}, info)))
		})
	}
}

func accessLevel(ctx context.Context, status int) slog.Level {
	switch {
	case clientHungUp(ctx):
		return slog.LevelWarn
	case status >= http.StatusInternalServerError:
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// causeLevel is the level of the line that says why the gateway answered a
// 5xx. A client that hung up is not a failure, so that is only a warning.
func causeLevel(ctx context.Context) slog.Level {
	if clientHungUp(ctx) {
		return slog.LevelWarn
	}
	return slog.LevelError
}

// clientHungUp reports whether net/http canceled the request because the
// client went away. A deadline of the gateway's own expires as
// context.DeadlineExceeded instead, and is still an error.
func clientHungUp(ctx context.Context) bool {
	return errors.Is(ctx.Err(), context.Canceled)
}

// noteUser tells the access log wrapping the request who the verified caller
// is.
func noteUser(ctx context.Context, userID string) {
	if info, ok := ctx.Value(requestInfoKey{}).(*requestInfo); ok {
		info.userID = userID
	}
}

// recoverPanics turns a panic into a 500 and one ERROR line, with the stack in
// a single field, so one bad request cannot take the gateway down.
func recoverPanics(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				rec := recover()
				if rec == nil {
					return
				}
				if err, ok := rec.(error); ok && errors.Is(err, http.ErrAbortHandler) {
					// net/http's own way to abort a response, which
					// ReverseProxy uses mid-body: let it reach the server.
					panic(rec)
				}
				log.LogAttrs(r.Context(), slog.LevelError, "panic serving request",
					slog.String("request_id", proxy.RequestIDFrom(r.Context())),
					slog.String("panic", fmt.Sprint(rec)),
					slog.String("stack", string(debug.Stack())))
				if !responseStarted(w) {
					writeError(w, http.StatusInternalServerError, "internal error")
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
