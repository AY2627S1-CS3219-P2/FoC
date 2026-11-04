// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-11-04
// Scope: The JSON access line written for every request and the panic
//   recoverer (D-135), modelled on order-service's middleware.
// Author review: James Teo — reviewed and verified.

package httpapi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/go-chi/chi/v5/middleware"
)

// msgInternal is all a client learns about a 500; the cause goes to the log.
const msgInternal = "internal server error"

type requestInfoKey struct{}

// requestInfo is how authenticate reports the verified caller back to the
// access log that wraps it.
type requestInfo struct {
	userID string
}

// accessLog writes one line per request once it has been answered: WARN when
// the client hung up, whatever the status, ERROR for a 5xx, INFO otherwise.
// The path is logged without its query string.
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
			level := slog.LevelInfo
			switch {
			case hungUp(r):
				level = slog.LevelWarn
			case status >= http.StatusInternalServerError:
				level = slog.LevelError
			}
			attrs := []slog.Attr{
				slog.String("request_id", middleware.GetReqID(r.Context())),
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", status),
				slog.Float64("duration_ms", float64(time.Since(start))/float64(time.Millisecond)),
			}
			if info.userID != "" {
				attrs = append(attrs, slog.String("user_id", info.userID))
			}
			log.LogAttrs(r.Context(), level, "http request", attrs...)
		})
	}
}

// hungUp reports whether the client of r went away before it was answered. A
// deadline this service sets itself ends in context.DeadlineExceeded instead,
// so it is never taken for one.
func hungUp(r *http.Request) bool {
	return errors.Is(r.Context().Err(), context.Canceled)
}

// recoverPanics turns a handler's panic into a 500 and one ERROR line with the
// stack, so one bad request cannot take the process down.
func recoverPanics(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				rec := recover()
				if rec == nil {
					return
				}
				if err, ok := rec.(error); ok && errors.Is(err, http.ErrAbortHandler) {
					// net/http's deliberate way to abort a response: it expects
					// the panic back and logs nothing for it.
					panic(rec)
				}
				log.LogAttrs(r.Context(), slog.LevelError, "panic serving request",
					slog.String("request_id", middleware.GetReqID(r.Context())),
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
// which a 500 can no longer be sent. w is the access log's writer.
func responseStarted(w http.ResponseWriter) bool {
	sw, ok := w.(interface{ Status() int })
	return ok && sw.Status() != 0
}
