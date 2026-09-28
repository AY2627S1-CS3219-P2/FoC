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
		logger.Error("internal server error",
			"request_id", middleware.GetReqID(ctx),
			"error", rawErr.Error(),
			"stack_trace", string(debug.Stack()),
		)
	}
	writeJSON(w, status, map[string]string{"error": clientMessage})
}

// decodeJSONBody decodes one request body after enforcing the recorded size limit.
func decodeJSONBody(w http.ResponseWriter, r *http.Request, value any) bool {
	// AI-generated (edited by ZI YANG): use the recorded 10 KiB boundary before decoding JSON.
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
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
