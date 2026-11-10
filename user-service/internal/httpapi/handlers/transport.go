// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-21
// Scope: Relocated shared JSON encoding for grouped HTTP handlers.
// Author review: COMPLETED BY ZI YANG

package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"runtime/debug"

	"github.com/go-chi/chi/v5/middleware"
)

const maxRequestBodyBytes int64 = 10 * 1024

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

// writeError logs internal diagnostics before writing a sanitized JSON error.
func writeError(ctx context.Context, w http.ResponseWriter, logger *slog.Logger, status int, rawErr error, clientMessage string) {
	// AI-generated (edited by ZI YANG): preserve request-correlated diagnostics only for recorded HTTP 500 responses.
	if status == http.StatusInternalServerError && rawErr != nil && logger != nil {
		logger.Log(ctx, causeLevel(ctx), "internal server error",
			"request_id", middleware.GetReqID(ctx),
			"error", rawErr.Error(),
			"stack_trace", string(debug.Stack()),
		)
	}
	writeJSON(w, status, map[string]string{"error": clientMessage})
}

// causeLevel is the level a 5xx's cause is logged at: WARN when the client
// hung up, since then nothing in the service failed, and ERROR otherwise.
func causeLevel(ctx context.Context) slog.Level {
	if clientHungUp(ctx) {
		return slog.LevelWarn
	}
	return slog.LevelError
}

// decodeJSONBody decodes one request body after enforcing the recorded size limit.
func decodeJSONBody(w http.ResponseWriter, r *http.Request, value any) bool {
	// AI-generated (edited by ZI YANG): use the recorded 10 KiB boundary before decoding JSON.
	r.Body = http.MaxBytesReader(innermostWriter(w), r.Body, maxRequestBodyBytes)
	if err := json.NewDecoder(r.Body).Decode(value); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "request body too large"})
		} else {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		}
		return false
	}
	return true
}

// innermostWriter follows Unwrap, as http.ResponseController does, to the
// writer net/http made for the request. Only through that writer can
// http.MaxBytesReader drop the connection of an oversized body, and it does
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
