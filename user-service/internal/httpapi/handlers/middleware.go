// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-11-04
// Scope: The JSON access line every request writes, and the recoverer that
//   turns a handler panic into a 500 and one JSON ERROR line.
// Author review: James Teo — reviewed and verified.

package handlers

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
)

type requestInfoKey struct{}

// requestInfo is how RequireJWT and Logout, which run inside AccessLog, report
// the verified caller back to it.
type requestInfo struct {
	userID string
}

// AccessLog writes one line for each request once it completes: its id,
// method, path, status, duration and, when known, the caller. The path leaves
// out the query string, which can carry what must never be logged.
func AccessLog(logger *slog.Logger) func(http.Handler) http.Handler {
	logger = orDiscard(logger)
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
				slog.String("request_id", middleware.GetReqID(r.Context())),
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", status),
				slog.Float64("duration_ms", float64(time.Since(start))/float64(time.Millisecond)),
			}
			if info.userID != "" {
				attrs = append(attrs, slog.String("user_id", info.userID))
			}
			logger.LogAttrs(r.Context(), accessLevel(r.Context(), status), "http request", attrs...)
		})
	}
}

// accessLevel is WARN for a request whose client hung up, whatever its
// status, ERROR for any other 5xx, and INFO otherwise.
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

// RecoverPanics turns a handler panic into a 500 and one ERROR line with the
// request id and the stack, in place of net/http's plain-text report.
func RecoverPanics(logger *slog.Logger) func(http.Handler) http.Handler {
	logger = orDiscard(logger)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ww, ok := w.(middleware.WrapResponseWriter)
			if !ok {
				ww = middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			}
			defer func() {
				rec := recover()
				if rec == nil {
					return
				}
				if err, ok := rec.(error); ok && errors.Is(err, http.ErrAbortHandler) {
					// net/http's own way to abort a response; it expects the
					// panic back and logs nothing for it.
					panic(rec)
				}
				logger.LogAttrs(r.Context(), slog.LevelError, "panic serving request",
					slog.String("request_id", middleware.GetReqID(r.Context())),
					slog.String("panic", fmt.Sprint(rec)),
					slog.String("stack_trace", string(debug.Stack())))
				if ww.Status() == 0 {
					writeJSON(ww, http.StatusInternalServerError, map[string]string{"error": http.StatusText(http.StatusInternalServerError)})
				}
			}()
			next.ServeHTTP(ww, r)
		})
	}
}

// recordCaller names the verified caller on the request's access line.
func recordCaller(ctx context.Context, userID uuid.UUID) {
	if info, ok := ctx.Value(requestInfoKey{}).(*requestInfo); ok {
		info.userID = userID.String()
	}
}

// clientHungUp reports whether the client went away before the request
// finished: net/http cancels the request's context when it does. A deadline
// of the service's own ends a context with DeadlineExceeded instead, so it is
// never mistaken for one.
func clientHungUp(ctx context.Context) bool {
	return errors.Is(ctx.Err(), context.Canceled)
}

func orDiscard(logger *slog.Logger) *slog.Logger {
	if logger == nil {
		return slog.New(slog.DiscardHandler)
	}
	return logger
}
