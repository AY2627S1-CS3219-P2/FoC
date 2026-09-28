// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-21
// Scope: Relocated shared JSON encoding for grouped HTTP handlers.
// Author review: COMPLETED BY ZI YANG

package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
)

const maxRequestBodyBytes int64 = 10 * 1024

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

// decodeJSONBody decodes one request body after enforcing the recorded size limit.
func decodeJSONBody(w http.ResponseWriter, r *http.Request, value any) bool {
	// AI-generated (edited by PENDING): use the recorded 10 KiB boundary before decoding JSON.
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
