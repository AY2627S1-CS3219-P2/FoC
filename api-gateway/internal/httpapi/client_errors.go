// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-11-04
// Scope: POST /api/client-errors, the gateway's own handler for a signed-in
//   browser's error reports: the body read, checked and logged as one line.
// Author review: James Teo — reviewed and verified.

package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"foc/api-gateway/internal/proxy"
)

// maxClientErrorBytes caps a report's body. The browser sends at most a
// 500-character message and a 2 KB stack, so a real report fits well within
// it.
const maxClientErrorBytes = 8 << 10

// clientErrorReport is one uncaught error a browser reports.
type clientErrorReport struct {
	kind, message, stack, view string
}

// clientErrors answers POST /api/client-errors. It runs behind RequireToken,
// so only a signed-in caller reports, and it writes the report as one WARN
// line naming that caller, then answers 204. A body over maxClientErrorBytes
// is a 413; a body that is not a valid report is a 400.
func clientErrors(log *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(http.MaxBytesReader(innermostWriter(w), r.Body, maxClientErrorBytes))
		var tooLarge *http.MaxBytesError
		switch {
		case errors.As(err, &tooLarge):
			writeError(w, http.StatusRequestEntityTooLarge, "report too large")
			return
		case err != nil:
			writeError(w, http.StatusBadRequest, "malformed report")
			return
		}
		report, err := decodeClientError(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "malformed report")
			return
		}
		if !knownClientErrorKind(report.kind) {
			writeError(w, http.StatusBadRequest, "unknown report kind")
			return
		}

		ctx := r.Context()
		caller, _ := proxy.IdentityFrom(ctx)
		log.LogAttrs(ctx, slog.LevelWarn, "browser error",
			slog.String("source", "browser"),
			slog.String("user_id", caller.UserID),
			slog.String("request_id", proxy.RequestIDFrom(ctx)),
			slog.String("kind", report.kind),
			slog.String("message", report.message),
			slog.String("stack", report.stack),
			slog.String("view", report.view))
		w.WriteHeader(http.StatusNoContent)
	}
}

// innermostWriter follows Unwrap, as http.ResponseController does, to the
// writer net/http made for the request. Only through that writer can
// http.MaxBytesReader close the connection of an oversized body, and it does
// not unwrap the access log's writer itself. The 413 still goes through w, so
// the access line records it.
func innermostWriter(w http.ResponseWriter) http.ResponseWriter {
	for {
		wrapper, ok := w.(interface{ Unwrap() http.ResponseWriter })
		if !ok {
			return w
		}
		w = wrapper.Unwrap()
	}
}

// decodeClientError parses a report: one JSON object whose only keys are kind,
// message, stack and view, each a string, and a key left out is empty. Keys
// are matched exactly, which decoding into a struct would not do:
// encoding/json matches field names case-insensitively.
func decodeClientError(raw []byte) (clientErrorReport, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return clientErrorReport{}, err
	}
	var report clientErrorReport
	for key, value := range fields {
		var field *string
		switch key {
		case "kind":
			field = &report.kind
		case "message":
			field = &report.message
		case "stack":
			field = &report.stack
		case "view":
			field = &report.view
		default:
			return clientErrorReport{}, fmt.Errorf("unknown field %q", key)
		}
		if err := json.Unmarshal(value, field); err != nil {
			return clientErrorReport{}, fmt.Errorf("%s: %w", key, err)
		}
	}
	return report, nil
}

// knownClientErrorKind reports whether kind is one the browser sends: a render
// error React caught, an uncaught error event, or an unhandled promise
// rejection.
func knownClientErrorKind(kind string) bool {
	switch kind {
	case "render", "error", "rejection":
		return true
	}
	return false
}
