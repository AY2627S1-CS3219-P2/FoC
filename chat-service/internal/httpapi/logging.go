// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-11-05
// Scope: The JSON access line every request gets, the response wrapper that
//   records its status and lets a WebSocket upgrade through, the panic
//   recoverer and the 5xx cause line (D-135), modelled on order-service's
//   middleware.
// Author review: James Teo — reviewed and verified.

package httpapi

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"runtime/debug"
	"time"

	"foc/chat-service/internal/auth"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
)

type requestInfoKey struct{}

// requestInfo is how the handlers inside accessLog report the verified caller
// back to it.
type requestInfo struct {
	userID string
}

// accessLog writes one line per request once it has been answered: WARN when
// the client hung up, whatever the status, ERROR for a 5xx, INFO otherwise.
// The path is logged without its query string, which can carry a WebSocket
// ticket.
func accessLog(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			info := &requestInfo{}
			sw := &statusWriter{ResponseWriter: w}
			next.ServeHTTP(sw, r.WithContext(context.WithValue(r.Context(), requestInfoKey{}, info)))

			status := sw.status
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

// recordCaller names the verified caller on the request's access line.
func recordCaller(ctx context.Context, userID uuid.UUID) {
	if info, ok := ctx.Value(requestInfoKey{}).(*requestInfo); ok {
		info.userID = userID.String()
	}
}

// callerOnAccessLine names the caller auth.RequireToken verified on the
// request's access line.
func callerOnAccessLine(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if caller, ok := auth.CallerFromContext(r.Context()); ok {
			recordCaller(r.Context(), caller.UserID)
		}
		next.ServeHTTP(w, r)
	})
}

// statusWriter records the status a request is answered with. It passes
// http.Hijacker and http.Flusher through, so a WebSocket upgrade still works
// behind it.
type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	// A 1xx other than 101 is informational: the final status is still to come.
	if w.status == 0 && (code >= http.StatusOK || code == http.StatusSwitchingProtocols) {
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(b)
}

func (w *statusWriter) Flush() {
	f, ok := w.ResponseWriter.(http.Flusher)
	if !ok {
		return
	}
	if w.status == 0 {
		w.status = http.StatusOK
	}
	f.Flush()
}

// Hijack hands the connection to a WebSocket upgrade, which writes its 101
// straight onto the connection, past this writer; so 101 is recorded here.
func (w *statusWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, http.ErrNotSupported
	}
	conn, rw, err := h.Hijack()
	if err == nil && w.status == 0 {
		w.status = http.StatusSwitchingProtocols
	}
	return conn, rw, err
}

// Unwrap lets http.ResponseController reach the writer underneath.
func (w *statusWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

// recoverPanics turns a handler's panic into a 500 and one ERROR line with the
// request id and the stack, so one bad request cannot take the process down.
func recoverPanics(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				rec := recover()
				if rec == nil {
					return
				}
				if err, ok := rec.(error); ok && errors.Is(err, http.ErrAbortHandler) {
					// net/http's own way to abort a response: it expects the
					// panic back and logs nothing for it.
					panic(rec)
				}
				log.LogAttrs(r.Context(), slog.LevelError, "panic serving request",
					slog.String("request_id", middleware.GetReqID(r.Context())),
					slog.String("panic", fmt.Sprint(rec)),
					slog.String("stack", string(debug.Stack())))
				if !responseStarted(w) {
					writeError(w, http.StatusInternalServerError, "internal server error")
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// responseStarted reports whether a status has gone out on w, the access
// log's writer, after which a 500 can no longer be sent. A hijacked
// connection counts as started.
func responseStarted(w http.ResponseWriter) bool {
	sw, ok := w.(*statusWriter)
	return ok && sw.status != 0
}

// serverError answers a 500 with msg and logs err as its cause, once, with the
// request id. Once the client has hung up nothing is logged here: that is no
// fault of this service, and the access line records it at WARN.
func (h *Handler) serverError(w http.ResponseWriter, r *http.Request, msg string, err error) {
	if !hungUp(r) {
		h.log.LogAttrs(r.Context(), slog.LevelError, "request failed",
			slog.String("request_id", middleware.GetReqID(r.Context())),
			slog.String("err", err.Error()))
	}
	writeError(w, http.StatusInternalServerError, msg)
}
