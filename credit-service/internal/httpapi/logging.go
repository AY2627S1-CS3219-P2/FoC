// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-11-04
// Scope: The access line every request writes, the JSON panic recoverer, and
//   the ERROR line recording why a request was answered with a 5xx (D-135).
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

	"foc/credit-service/internal/gen"
	"foc/credit-service/internal/observability"

	"github.com/go-chi/chi/v5/middleware"
)

type requestInfoKey struct{}

// requestInfo lets RequireToken, which runs inside accessLog, hand the caller
// it verified back to the access line.
type requestInfo struct {
	userID string
}

// accessLog writes one line per request once it completes.
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
			case clientGone(r.Context()):
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

// recoverPanics turns a handler panic into a 500 and one ERROR line carrying
// the stack, so one bad request cannot take the process down.
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
					// the panic back, and logs nothing for it.
					panic(rec)
				}
				log.LogAttrs(r.Context(), slog.LevelError, "panic serving request",
					slog.String("request_id", middleware.GetReqID(r.Context())),
					slog.String("panic", observability.Sanitize(fmt.Sprint(rec))),
					slog.String("stack", string(debug.Stack())))
				if !responseStarted(w) {
					writeError(w, http.StatusInternalServerError, gen.INTERNALERROR, "internal error")
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

// logCause records why r is being answered with a 5xx: once, at ERROR, with the
// request id. A client that hung up is not a fault here, so its request is left
// to the access line, which logs it at WARN.
func logCause(log *slog.Logger, r *http.Request, msg string, err error) {
	ctx := r.Context()
	if clientGone(ctx) {
		return
	}
	attrs := []slog.Attr{slog.String("request_id", middleware.GetReqID(ctx))}
	if err != nil {
		attrs = append(attrs, slog.String("error", observability.Sanitize(err.Error())))
	}
	log.LogAttrs(ctx, slog.LevelError, msg, attrs...)
}

// clientGone reports whether the client hung up: net/http cancels a request's
// context when its connection closes. A deadline of the service's own ends a
// context with DeadlineExceeded instead, which is still a failure.
func clientGone(ctx context.Context) bool {
	return errors.Is(ctx.Err(), context.Canceled)
}

// orDiscard lets the exported constructors take a nil logger, as tests pass.
func orDiscard(log *slog.Logger) *slog.Logger {
	if log == nil {
		return slog.New(slog.DiscardHandler)
	}
	return log
}
